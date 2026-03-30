# kb-dev

**Local service manager for the KB Labs platform.**

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8.svg)](https://go.dev)
[![KB Labs](https://img.shields.io/badge/KB_Labs-platform-7C3AED.svg)](https://github.com/KirillBaranov)

kb-dev manages local development services with proper process tracking, health checks, dependency ordering, and auto-restart. It replaces the bash-based `dev.sh` with a single Go binary.

## Features

- ✅ **Process group management** — real PID tracking via `Setpgid`, no lsof guessing
- ✅ **Health probes with latency** — HTTP, TCP, and command probes with response time tracking
- ✅ **Dependency-aware parallel start** — topological sort, goroutine per service
- ✅ **Auto-restart on crash** — watchdog with exponential backoff (1s → 30s, max 5 retries)
- ✅ **Rich PID files** — JSON with user, timestamp, command (not bare numbers)
- ✅ **Environment resolution** — cached node/pnpm paths, no login shell dependency
- ✅ **Agent-first JSON protocol** — `ok` field, `hint` commands, `depsState`, `logsTail`
- ✅ **JSONL streaming** — real-time event monitoring via `watch`
- ✅ **Docker/Colima** — auto-detect and start Docker runtime on macOS

## Quick Start

```bash
# Build from source
cd infra/kb-labs-dev
make build

# Check environment
./kb-dev doctor

# Start all services
./kb-dev start

# Status with latency
./kb-dev status

# Agent-friendly: ensure services are alive (idempotent)
./kb-dev ensure rest gateway --json
```

## How It Works

```
                    ┌─────────────┐
                    │ dev.config   │  .kb/dev.config.json
                    │   .json      │  (12 services, 6 groups)
                    └──────┬──────┘
                           │
                    ┌──────▼──────┐
                    │   Config    │  Parse, validate, toposort
                    │   Parser    │
                    └──────┬──────┘
                           │
              ┌────────────┼────────────┐
              ▼            ▼            ▼
        ┌──────────┐ ┌──────────┐ ┌──────────┐
        │ Environ  │ │  Health  │ │  Docker  │
        │ Resolve  │ │  Probes  │ │  Colima  │
        └────┬─────┘ └────┬─────┘ └────┬─────┘
             │            │            │
             └────────────┼────────────┘
                          ▼
                   ┌─────────────┐
                   │   Manager   │  Start/Stop/Ensure/Ready/Watch
                   │             │  Dependency graph, parallel layers
                   └──────┬──────┘
                          │
              ┌───────────┼───────────┐
              ▼           ▼           ▼
        ┌──────────┐ ┌──────────┐ ┌──────────┐
        │ Process  │ │ Service  │ │ Watchdog │
        │  Spawn   │ │  State   │ │  Auto-   │
        │ + Kill   │ │ Machine  │ │ restart  │
        └──────────┘ └──────────┘ └──────────┘
```

**PID-first approach:** kb-dev knows the PID because it spawned the process. No more `lsof` guessing — port checks are only used for `--force` and `doctor`.

**State machine per service:** `dead` → `starting` → `alive` | `failed` → `stopping` → `dead`. Each transition is validated. No more if/else chains.

## Commands

### Core Commands

| Command | Description |
|---------|-------------|
| `kb-dev start [target]` | Start services with dependency resolution |
| `kb-dev stop [target]` | Stop services (optionally cascade dependents) |
| `kb-dev restart [target]` | Restart with dependent cascade |
| `kb-dev status` | Service status table with latency |
| `kb-dev health` | Run health probes on all services |
| `kb-dev logs <service>` | View service logs (with `--follow`) |
| `kb-dev doctor` | Environment diagnostics |

### Agent Commands

| Command | Description |
|---------|-------------|
| `kb-dev ensure <targets...>` | Idempotent desired state (alive→skip, dead→start, failed→restart) |
| `kb-dev ready <targets...>` | Block until all services are alive (gate) |
| `kb-dev watch` | Stream lifecycle events in real-time |

### Flags

| Flag | Description |
|------|-------------|
| `--json` | Structured JSON output (all commands) |
| `--force` | Kill port conflicts before starting |
| `--cascade` | Stop/restart dependent services too |
| `--no-cascade` | Skip dependent cascade on restart |
| `--config <path>` | Override config file path |

### Start

```bash
kb-dev start                    # all services
kb-dev start infra              # infrastructure group
kb-dev start rest               # single service (with deps)
kb-dev start --force            # kill port conflicts
kb-dev start --watch            # stay alive, auto-restart on crash
```

### Status

```bash
kb-dev status                   # human-readable table
kb-dev status --json            # machine-readable with depsState
```

Human output:
```
KB Labs Services

  [infra]
  ● qdrant              6333    alive       http://localhost:6333  12m   5ms
  ● redis               6379    alive                              12m   1ms
  ● state-daemon        7777    alive       http://localhost:7777  11m   6ms

  [backend]
  ◉ workflow            7778    starting    http://localhost:7778  3s
  ○ rest                5050    dead        http://localhost:5050
  ✕ gateway             4000    failed      http://localhost:4000
    ↳ health check timeout after 30s

  1 alive  1 starting  1 failed  9 dead  (12 total)
```

### Ensure (Agent-Friendly)

```bash
# Idempotent: "make sure rest is alive, handle everything"
kb-dev ensure rest gateway --json
```

```json
{
  "ok": true,
  "actions": [
    {"service": "redis", "action": "skipped", "reason": "already alive"},
    {"service": "state-daemon", "action": "skipped", "reason": "already alive"},
    {"service": "workflow", "action": "started", "elapsed": "2.1s"},
    {"service": "rest", "action": "started", "elapsed": "3.4s"},
    {"service": "gateway", "action": "started", "elapsed": "1.8s"}
  ]
}
```

### Doctor

```bash
kb-dev doctor --json
```

```json
{
  "ok": false,
  "checks": [
    {"id": "config", "ok": true, "detail": "12 services, 6 groups"},
    {"id": "docker", "ok": true, "detail": "Docker 28.4.0"},
    {"id": "node", "ok": true, "detail": "v20.19.4", "path": "/Users/.../.nvm/.../node"},
    {"id": "port:3000", "ok": false, "detail": "occupied (PID 2454), needed by studio"}
  ],
  "hint": "Port 3000 occupied. Fix: kb-dev stop studio --force"
}
```

### Watch (JSONL Streaming)

```bash
kb-dev watch --json
```

```
{"event":"health","service":"rest","ok":true,"latency":"45ms","ts":"2026-03-30T14:30:00Z"}
{"event":"crashed","service":"rest","exitCode":1,"logsTail":["Error: ..."],"ts":"..."}
{"event":"restarting","service":"rest","attempt":1,"backoff":"1s","ts":"..."}
{"event":"alive","service":"rest","latency":"50ms","ts":"..."}
```

## Agent Protocol

Every JSON response follows a consistent contract:

| Field | Type | When | Description |
|-------|------|------|-------------|
| `ok` | bool | always | Operation succeeded |
| `hint` | string | on failure | Exact command to fix the issue |
| `actions` | array | on mutations | What was done per service |
| `logsTail` | array | on failure | Last 5 log lines (no second call needed) |
| `depsState` | map | in status | Dependency states at a glance |

**Agent workflow:**
```bash
kb-dev doctor --json          # 1. diagnose
kb-dev ensure rest --json     # 2. bring up what I need
kb-dev ready rest --json      # 3. wait until ready
# ... work ...
kb-dev status --json          # 4. check state
```

## Configuration

kb-dev reads `.kb/dev.config.json` (auto-discovered by walking up from cwd):

```json
{
  "services": {
    "state-daemon": {
      "name": "State Daemon",
      "type": "node",
      "command": "node ./platform/.../dist/bin.cjs",
      "healthCheck": "http://localhost:7777/health",
      "port": 7777,
      "dependsOn": ["redis"],
      "env": {"KB_STATE_DAEMON_PORT": "7777"}
    }
  },
  "groups": {
    "infra": ["qdrant", "redis", "state-daemon"],
    "backend": ["workflow", "rest", "gateway"]
  },
  "settings": {
    "logsDir": ".kb/logs/tmp",
    "pidDir": ".kb/tmp",
    "startTimeout": 30000,
    "healthCheckInterval": 1000
  }
}
```

## Architecture

```
infra/kb-labs-dev/
├── main.go                         entry point (build-time version injection)
├── cmd/
│   ├── root.go                     cobra root, global flags, config discovery
│   ├── start.go                    start command
│   ├── stop.go                     stop command
│   ├── restart.go                  restart command
│   ├── status.go                   status table (human + JSON)
│   ├── health_cmd.go               health probe report
│   ├── logs.go                     log viewer (tail + follow)
│   ├── doctor.go                   environment diagnostics
│   ├── ensure.go                   idempotent desired state
│   ├── ready.go                    blocking readiness gate
│   ├── watch.go                    JSONL event streaming
│   ├── helpers.go                  shared loadManager, errSilent
│   └── output.go                   lipgloss formatting
└── internal/
    ├── config/                     parse dev.config.json, toposort, validation
    ├── environ/                    resolve node/pnpm paths, env cache
    ├── health/                     HTTP/TCP/Command probes + latency
    ├── process/                    spawn (Setpgid), kill (group), rich PID files
    ├── service/                    state machine, ServiceRunner interface
    ├── manager/                    orchestration, deps, watchdog, events
    ├── docker/                     Docker availability, Colima auto-start
    └── logger/                     per-service log files, tail, follow
```

### Extension Points

| Point | How to Extend |
|-------|--------------|
| New service runner | Implement `service.Runner` interface (e.g., DockerComposeRunner, RemoteRunner) |
| New health probe | Add probe type in `health/probe.go`, extend `ClassifyProbe` |
| New CLI command | Add `cmd/<name>.go` with cobra command, register in `init()` |
| Config migration | Bump config version, add migration logic in `config.Load()` |
| Remote API | Wrap `Manager` methods in HTTP handlers (same JSON schema) |

## Development

```bash
# Prerequisites: Go 1.24+, golangci-lint

# Build
make build

# Run tests (with race detector)
make test

# Run linter
make lint

# Coverage report
make cover

# Build for all platforms (via goreleaser)
make snapshot
```

## Comparison with dev.sh

| Aspect | dev.sh | kb-dev |
|--------|--------|--------|
| Process tracking | `lsof` port check | PID from `cmd.Start()` |
| Kill mechanism | Recursive `pgrep -P` | `syscall.Kill(-pgid)` |
| Health check missing | Assumes healthy | Reports dead |
| Alien process on port | "alive" | "dead" (correct) |
| Auto-restart | None | Watchdog with backoff |
| Parallel start | Serial loop | Goroutine per service |
| Agent support | `--json` flag only | `ensure`, `ready`, `watch`, `depsState`, `hint` |
| PID files | Bare number | JSON (user, timestamp, command) |
| Shell dependency | bash, jq, curl, lsof | Single Go binary |

## License

[MIT](LICENSE)
