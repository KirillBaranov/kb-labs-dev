package process

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// ResourceUsage holds CPU and memory stats for a process.
type ResourceUsage struct {
	CPUPercent float64 // e.g. 58.1
	RSSBytes   int64   // resident set size in bytes
}

// GetResourceUsage reads CPU% and RSS for a given PID using ps.
// Returns nil if the process is not found.
func GetResourceUsage(pid int) *ResourceUsage {
	if pid <= 0 {
		return nil
	}

	// ps -p PID -o %cpu=,rss= → "58.1 205856\n"
	// rss is in kilobytes on macOS and Linux.
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "%cpu=,rss=").Output()
	if err != nil {
		return nil
	}

	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) < 2 {
		return nil
	}

	cpu, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return nil
	}

	rssKB, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return nil
	}

	return &ResourceUsage{
		CPUPercent: cpu,
		RSSBytes:   rssKB * 1024,
	}
}

// FormatMemory formats bytes into a human-readable string.
func FormatMemory(bytes int64) string {
	switch {
	case bytes >= 1024*1024*1024:
		return fmt.Sprintf("%.1fGB", float64(bytes)/(1024*1024*1024))
	case bytes >= 1024*1024:
		return fmt.Sprintf("%dMB", bytes/(1024*1024))
	case bytes >= 1024:
		return fmt.Sprintf("%dKB", bytes/1024)
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}
