package process

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const pollInterval = 100 * time.Millisecond

// KillGroup sends SIGTERM to the entire process group, waits up to gracePeriod,
// then sends SIGKILL if the group is still alive.
func KillGroup(pgid int, gracePeriod time.Duration) error {
	// Send SIGTERM to the process group (negative pgid).
	if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil {
		// ESRCH = no such process — already dead.
		if err == syscall.ESRCH {
			return nil
		}
		return fmt.Errorf("SIGTERM to pgid %d: %w", pgid, err)
	}

	// Poll until dead or grace period expires.
	deadline := time.Now().Add(gracePeriod)
	for time.Now().Before(deadline) {
		if !groupAlive(pgid) {
			return nil
		}
		time.Sleep(pollInterval)
	}

	// Still alive — force kill.
	if groupAlive(pgid) {
		if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
			return fmt.Errorf("SIGKILL to pgid %d: %w", pgid, err)
		}
		time.Sleep(pollInterval) // brief wait for cleanup
	}

	return nil
}

// KillPort finds processes LISTENING on the given port and kills them.
// Used for --force cleanup of alien processes.
func KillPort(port int) error {
	pids := GetListenerPIDs(port)
	if len(pids) == 0 {
		return nil
	}

	for _, pid := range pids {
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}

	time.Sleep(1 * time.Second)

	for _, pid := range pids {
		if IsAlive(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}

	return nil
}

// IsAlive checks if a process with the given PID exists.
func IsAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// groupAlive checks if any process in the group is still alive.
func groupAlive(pgid int) bool {
	return syscall.Kill(-pgid, 0) == nil
}

// GetListenerPIDs finds PIDs listening on a TCP port using lsof.
func GetListenerPIDs(port int) []int {
	out, err := exec.Command("lsof", "-ti", fmt.Sprintf(":%d", port), "-sTCP:LISTEN").Output()
	if err != nil {
		// lsof returns exit 1 when no matches — not an error.
		return nil
	}

	var pids []int
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pid, err := strconv.Atoi(line)
		if err != nil {
			continue
		}
		pids = append(pids, pid)
	}
	return pids
}
