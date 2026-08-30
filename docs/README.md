# QManager Documentation

Welcome to the technical documentation for **QManager** — the standalone single-binary web interface, telemetry daemon, and modem management engine for Qualcomm-based Quectel cellular modems (RM520N-GL & RG501Q-EU).

---

## Documentation Index

| Document | Description |
|----------|-------------|
| [Single-Binary Architecture](REWRITE_ARCHITECTURE.md) | Complete architectural specifications for the Go standalone binary, Chi HTTP router, memory footprint, and background daemons |
| [Frontend Guide](FRONTEND.md) | Next.js 15 App Router architecture, UI component structure, custom hooks, state management, and build pipeline |
| [Hardware Platform Profile & Matrix](reference/platform-profile.md) | SoC detection (SDX55 vs SDX65), form-factor derivation, hardware capabilities, and boot self-heal verification |
| [AT Command Engine & Transport](reference/at-command-transport.md) | Pure-Go direct character device transport (`/dev/smd11`), command serialization, and 3GPP terminator parsing |
| [Monotonic Scheduler & Clock-Step Guard](reference/scheduled-timers.md) | 1970 uncalibrated RTC boot-step protection, scheduled reboots, and tower locking timelines |
| [Connection Watchdog](reference/connection-watchdog.md) | 4-tier link failure recovery pipeline, probing parameters, and cooldown mechanics |
| [Carrier Aggregation & Radio Stats](reference/carrier-aggregation.md) | Primary & Secondary component carrier parsing (+QCAINFO, +QENG), bandwidths, and signal metrics |
| [Band & Tower Locking](reference/tower-locking.md) | EARFCN/ARFCN cell locking, PCI targeting, and failover state machines |

---

## Tech Stack Overview

| Component | Technology / Implementation |
|---|---|
| **Core Daemon & Web Engine** | Go 1.22+ compiled as static binary with `CGO_ENABLED=0` |
| **HTTP Router & API** | Chi HTTP router with sub-millisecond response latency and full `/cgi-bin/quecmanager/*` compatibility |
| **Frontend Framework** | Next.js 15 (React 19, App Router, static export embedded via `embed.FS`) |
| **UI Components & Styling** | shadcn/ui (Radix primitives), Tailwind CSS v4, OKLCH theme engine |
| **AT Command Transport** | Pure-Go direct character device access on `/dev/smd11` with mutex serialization |
| **Telemetry & Metrics** | 1 Hz background signal poller, UDP/TCP latency prober (1.1.1.1:53), `/proc/uptime` & `/proc/meminfo` accounting |
| **Persistence & Config** | JSON configuration store (`/etc/qmanager/qmanager.conf`, `/etc/qmanager/tower_lock.json`) |
| **Target Platforms** | Quectel RM520N-GL (SDX65, ARMv7l) & Quectel RG501Q-EU (SDX55, ARMv7l) running embedded Linux |

---

## Development & Build Workflow

### Local Development
```bash
# 1. Start Frontend Dev Server
cd frontend
bun install
bun run dev

# 2. Run Go Backend with Mock Transport
cd backend
go run ./cmd/qmanager
```

### Production Cross-Compilation
```bash
# Build standalone ARMv7 binary
make build-backend

# Build release package with embedded Next.js frontend
make package
```
