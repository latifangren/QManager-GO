# 📐 QManager Single-Binary Go Backend & Monorepo Architecture

> **Target Platform:** Quectel RG501Q-EU (Qualcomm SDX55 Cortex-A7, Linux Yocto, systemd, BusyBox 1.29.3, UBIFS NAND) & Quectel RM520N-GL (SDX65 Cortex-A7, Linux 5.4).  
> **Goal:** Replace CGI scripts, Lighttpd, Rust ping daemon, and multiple shell daemons with **one single standalone Go binary** embedding the Next.js static web UI.

---

## 1. Executive Summary & Problems Solved

| Existing Architecture (CGI + Shell) | New Go Single Binary Architecture |
| :--- | :--- |
| **High CPU/RAM Fork Overhead:** Every API hit forks `/bin/sh`, `jq`, `qcmd`, and `atcli_smd11`. | **Zero Fork Overhead:** Native HTTP router (Chi) with pure Go JSON serialization. |
| **Fragile AT Locking:** Multiple bash scripts competing via `/tmp/qmanager_at.lock` + `flock`. | **3-Tier Priority Actor Queue:** Dedicated priority queue on `/dev/smd11` (High: Recovery, Normal: UI, Low: 1Hz Telemetry). |
| **Unbounded NAND Flash Wear:** Shell scripts logging continuously to flash storage. | **Zero Flash Wear (RAM-First):** In-memory ring buffer logging, memory telemetry buffers, atomic debounced config writes. |
| **Complex Multi-Component Stack:** Lighttpd + Entware + PHP/CGI + Shell Daemons + Rust binary (`qmanager_ping`) + `sms_tool`. | **1 Single Static Binary:** Direct execution via systemd (`/usrdata/qmanager/qmanager`). |
| **Messy Repo Root:** `app/`, `components/`, `hooks/`, `scripts/`, `installer-gui/` all mixed in root. | **Clean Modular Workspace:** Separated `frontend/` (Next.js 15), `backend/` (Go), `pkg/`, `deploy/`. |

---

## 2. Target Directory Structure

```text
QManager/
├── frontend/                     # Next.js 15 Static Export Frontend
│   ├── app/                      # Next.js App Router
│   ├── components/               # React components (shadcn/ui + Radix)
│   ├── hooks/                    # React Query / SWR / Fetching hooks
│   ├── types/                    # TypeScript data definitions
│   ├── lib/                      # Utilities
│   └── out/                      # Static export output (HTML/JS/CSS/SVG)
│
├── backend/                      # Pure Go Backend
│   ├── cmd/
│   │   └── qmanager/
│   │       ├── main.go           # Single binary entrypoint & service supervisor
│   │       └── dist/             # Embedded static frontend assets (//go:embed)
│   │
│   └── internal/
│       ├── atengine/             # AT command engine, parser & direct SMD11 transport
│       │   ├── transport.go      # Direct character device (/dev/smd11), CLI, mock
│       │   ├── engine.go         # 3-tier priority queue, mutex serialization & timeout
│       │   └── parser.go         # Low-allocation 3GPP & Quectel AT parser (QENG, QCAINFO, CSQ)
│       │
│       ├── telemetry/            # Background daemons & collectors (RAM-First)
│       │   ├── poller.go         # 1 Hz continuous signal poller (RSRP, RSRQ, SINR, CA)
│       │   ├── ping.go           # Low-overhead TCP/UDP latency & jitter prober
│       │   ├── watchdog.go       # 4-tier automated connection recovery engine
│       │   ├── scheduler.go      # Monotonic scheduler with 1970 clock-step guard
│       │   ├── sms_forwarder.go  # SMS inbox poller & multi-target dispatcher
│       │   └── alerts.go         # Alert dispatcher (Discord, Webhook, SMS, SMTP)
│       │
│       ├── platform/             # Hardware detection, sysfs metrics & self-heal
│       │   ├── detector.go       # Parses /etc/quectel-project-version & /proc/cmdline
│       │   ├── sysfs.go          # Reads /proc/uptime, /proc/meminfo, net/dev, thermal
│       │   └── self_heal.go      # Atomic /etc/qmanager/platform.json synchronizer
│       │
│       ├── config/               # Thread-safe persistent configuration
│       │   └── config.go         # Mutex-guarded atomic /etc/qmanager/qmanager.conf manager
│       │
│       └── api/                  # HTTP Router & Domain Handlers
│           ├── router/           # Chi router, auth middlewares, SPA static file server
│           └── handlers/         # REST API & CGI-compatible endpoints
│               ├── cellular.go   # Signal, carrier aggregation, radio power
│               ├── cellular_apn.go
│               ├── cellular_imei.go
│               ├── cellular_fplmn.go
│               ├── cellular_priority.go
│               ├── cellular_mbn.go
│               ├── frequency_lock.go
│               ├── frequency_calc.go
│               ├── tower_schedule.go
│               ├── sim_profiles.go
│               ├── scenarios.go
│               ├── cell_scanner.go
│               ├── neighbour_scanner.go
│               ├── speedtest.go
│               ├── network.go
│               ├── network_mtu.go
│               ├── ip_passthrough.go
│               ├── video_optimizer.go
│               ├── traffic_engine.go
│               ├── sms.go
│               ├── sms_forwarding.go
│               ├── system.go
│               ├── update.go
│               ├── logs.go       # In-memory circular ring buffer log reader
│               └── auth.go
│
├── deploy/                       # Device deployment scripts & systemd units
│   ├── systemd/
│   │   └── qmanager.service      # Standalone systemd unit
│   └── install.sh                # Direct device installer script
│
├── Makefile                      # Cross-compilation & packaging automation
└── docs/                         # Architecture, API & platform documentation
```

---

## 3. Runtime Concurrency & Subsystem Interaction

```text
┌─────────────────────────────────────────────────────────────────────────────────┐
│                           QManager Standalone Daemon                            │
│                                                                                 │
│  ┌─────────────────────────┐                 ┌───────────────────────────────┐  │
│  │   Embedded Web Server   │                 │   3-Tier Priority AT Queue    │  │
│  │     (Chi HTTP Router)   │                 │                               │  │
│  │                         │                 │ - High: Recovery / Watchdog   │  │
│  │ - Next.js Static SPA    │  Issues Command │ - Normal: User UI Mutations   │  │
│  │ - REST & CGI Endpoints  ├────────────────►│ - Low: 1 Hz Poller (Yields)   │  │
│  │ - Session Auth Guard    │                 │ - Direct /dev/smd11 FIFO      │  │
│  └─────────────────────────┘                 └───────────────┬───────────────┘  │
│                                                              │                  │
│  ┌─────────────────────────┐                                 │                  │
│  │   Background Daemons    │                                 │                  │
│  │   (RAM Ring Buffers)    │                                 │                  │
│  │ - Signal Poller (1 Hz)  ├─────────────────────────────────┘                  │
│  │ - Ping / Jitter Monitor │                                                    │
│  │ - 4-Tier Watchcat       │                                                    │
│  │ - Monotonic Scheduler   │                                                    │
│  │ - SMS Forwarder Engine  │                                                    │
│  └─────────────────────────┘                                                    │
│                                                                                 │
│  ┌─────────────────────────┐                                                    │
│  │ Platform & Metrics      │                                                    │
│  │ - /proc/uptime, meminfo │                                                    │
│  │ - /proc/net/dev traffic │                                                    │
│  │ - Thermal / SoC Temp    │                                                    │
│  │ - Boot Self-Heal Profile│                                                    │
│  └─────────────────────────┘                                                    │
└──────────────────────────────────────────────────────────────┬──────────────────┘
                                                               │
                                                               ▼
                               ┌───────────────────────────────────────────────┐
                               │           Linux Hardware Interfaces           │
                               │                                               │
                               │  - Serial: Direct Character Device /dev/smd11 │
                               │  - Network: rmnet_data0, wwan0, eth0          │
                               │  - Storage (Atomic writes): /etc/qmanager/    │
                               │  - Kernel: /proc/net/dev, /sys/class/thermal  │
                               └───────────────────────────────────────────────┘
```

---

## 4. Target Platform Matrix

| Attribute | Quectel RG501Q-EU (Primary) | Quectel RM520N-GL (Secondary) |
| :--- | :--- | :--- |
| **Baseband / SoC** | Qualcomm Snapdragon X55 (SDX55) | Qualcomm Snapdragon X65 (SDX65) |
| **CPU Architecture** | ARMv7l (32-bit Cortex-A7) | ARMv7l (32-bit Cortex-A7) |
| **Go Cross-compile** | `GOOS=linux GOARCH=arm GOARM=7` | `GOOS=linux GOARCH=arm GOARM=7` |
| **CGO Requirement** | **`CGO_ENABLED=0` (Pure Go)** | **`CGO_ENABLED=0` (Pure Go)** |
| **Init System** | systemd | systemd |
| **Rootfs Policy** | `/` (UBIFS ro), `/usrdata` (rw) | `/` (UBIFS ro), `/usrdata` (rw) |
| **Flash Wear Policy** | Zero continuous disk writes, RAM-First | Zero continuous disk writes, RAM-First |
| **Primary AT Channel** | Direct `/dev/smd11` character device | Direct `/dev/smd11` character device |

---

## 5. Comprehensive Rewrite Status & Roadmap

### [x] Phase 1: Monorepo Clean Architecture & Directory Structure
- [x] Separate codebase cleanly into `frontend/` (Next.js 15) and `backend/` (Go).
- [x] Configure Next.js static export (`output: "export"`) with direct build synchronization to `backend/cmd/qmanager/dist/`.
- [x] Initialize Go root module `qmanager` with zero CGO dependencies.
- [x] Create standardized `Makefile` with targets for `build-frontend`, `build-backend`, `build`, `package`, and `test`.

### [x] Phase 2: Core AT Engine & Pure-Go Direct SMD11 Transport
- [x] Implement thread-safe `Engine` (`internal/atengine`) with priority serialization and `context.Context` execution timeouts.
- [x] Implement pure-Go `DeviceTransport` accessing `/dev/smd11` (and `/dev/smd7` / `/dev/ttyUSB2`) directly with `os.OpenFile(O_RDWR)`.
- [x] Implement 3GPP AT response framing and termination detection (`OK`, `ERROR`, `+CME ERROR:`, `+CMS ERROR:`, `NO CARRIER`, `BUSY`).
- [x] Eliminate external ARM binary dependencies (`atcli_smd11` and `sms_tool`).
- [x] Implement low-allocation byte-scanning AT response parsers for `+QENG="servingcell"`, `+QCAINFO`, `+CSQ`, `+QRSRP`, `+CPIN?`, `+QNWINFO`, `+CFUN?`.
- [x] Provide `MockTransport` with `SetResponse(cmd, resp)` for hermetic unit testing.

### [x] Phase 3: Telemetry, In-Memory Poller, Background Daemons & Ping Monitor
- [x] Implement 1 Hz background `Poller` maintaining rolling in-memory signal and CA stats buffers in RAM.
- [x] Implement pure-Go `PingProber` executing lightweight TCP/UDP probes against `1.1.1.1:53` without spawning external ping processes.
- [x] Implement 4-tier `Watchdog` connection recovery worker (Radio toggle -> Network restart -> SIM failover -> Gated system reboot).
- [x] Implement Monotonic `Scheduler` with **1970 Clock-Step Guard** (`ClockSane`, `BootSettled`) to protect against uncalibrated RTC boot-step reboot traps.
- [x] Implement pure-Go `SMSForwarder` with in-memory deduplication cache.
- [x] Implement centralized multi-channel `Alerts` engine (Discord webhook, Telegram, HTTP webhook, Email SMTP, Cellular SMS).

### [x] Phase 4: Core HTTP Router, Embedded Static Frontend Server & Session Auth
- [x] Implement Chi HTTP router with sub-millisecond response latency.
- [x] Implement embedded SPA file server embedding `dist/` via `//go:embed dist/*` with client-side route fallback.
- [x] Implement authentication system with HTTP-only session cookie (`qmanager_session`), constant-time password validation, and brute-force rate limiting.
- [x] Implement legacy CGI backward-compatibility routing (`/cgi-bin/quecmanager/*`) alongside native `/api/*` endpoints.

### [x] Phase 5: Domain Handlers Migration
- [x] **Cellular Settings & Identity**: APN management (`cellular_apn.go`), IMEI modification with Luhn validation (`cellular_imei.go`), FPLMN blocked network management (`cellular_fplmn.go`), Network Priority RAT ordering (`cellular_priority.go`), MBN carrier profile management (`cellular_mbn.go`).
- [x] **Advanced Locking & Scenarios**: Frequency carrier locking (`frequency_lock.go`), EARFCN/ARFCN frequency calculator (`frequency_calc.go`), Tower locking schedule & failover (`tower_schedule.go`), ICCID-bound SIM profiles (`sim_profiles.go`), Connection scenarios (`scenarios.go`).
- [x] **Cell Scanner & Speedtest**: Full RF frequency cell scanner with long-running task tracking (`cell_scanner.go`), Neighbour cell scanner (`neighbour_scanner.go`), Speedtest execution manager (`speedtest.go`).
- [x] **Network & Traffic Engine**: IP Passthrough / Bridge mode (`ip_passthrough.go`), Custom MTU (`network_mtu.go`), Video Optimizer (`video_optimizer.go`), Traffic Engine DPI bypass (`traffic_engine.go`).
- [x] **SMS Center**: SMS inbox reading, composition, and deletion (`sms.go`), SMS forwarding configuration (`sms_forwarding.go`).
- [x] **System & Platform**: Hardware detection & atomic self-heal (`platform.json`), OTA update runner (`update.go`), In-memory ring buffer log viewer (`logs.go`), General configuration (`system.go`).

---

### [x] Phase 6: Frontend/Backend API Contract Sync & Missing CGI Route Coverage
- [x] **6.1 Full `ModemStatus` Telemetry JSON Schema Alignment**:
  - [x] Audit `internal/telemetry/poller.go` JSON output against `frontend/types/modem-status.ts`.
  - [x] Ensure full field parity for `signal`, `network`, `cell`, `ca`, `sim`, `traffic`, `system`, and `secondary_signals`.
- [x] **6.2 Missing CGI Shim Endpoints & Handlers**:
  - [x] `/cgi-bin/quecmanager/bands/failover_toggle.sh` & `failover_status.sh` (Band failover state machine).
  - [x] `/cgi-bin/quecmanager/monitoring/alerts.sh` (Alert notification settings & test triggers).
  - [x] `/cgi-bin/quecmanager/monitoring/watchdog.sh` (Watchcat configuration & status).
  - [x] `/cgi-bin/quecmanager/network/ethernet.sh` (Ethernet link speed & port status).
  - [x] `/cgi-bin/quecmanager/network/data_used.sh` & `data_used_reset.sh` (Persistent data usage accounting).
  - [x] `/cgi-bin/quecmanager/vpn/tailscale.sh` (Tailscale VPN daemon status & management).
  - [x] `/cgi-bin/quecmanager/public/overview.sh` & `hostname.sh` & `units.sh` (Public unauthenticated device summary).
  - [x] `/cgi-bin/quecmanager/system/settings.sh` & `sim_registry.sh` (System preferences & known SIMs).
- [x] **6.3 Language Packs & Diagnostic Health Check**:
  - [x] `/cgi-bin/quecmanager/system/language-packs/list.sh`, `install.sh`, `install_status.sh`, `remove.sh`.
  - [x] `/cgi-bin/quecmanager/system/health-check/run.sh`, `status.sh`, `download.sh`, `clear.sh`.
- [x] **6.4 Telemetry History Endpoints**:
  - [x] `/cgi-bin/quecmanager/at_cmd/fetch_signal_history.sh` (Rolling RSRP/RSRQ/SINR chart points).
  - [x] `/cgi-bin/quecmanager/at_cmd/fetch_ping_history.sh` (Rolling latency & jitter points).
  - [x] `/cgi-bin/quecmanager/at_cmd/fetch_events.sh` (Modem connection/disconnection event logs).

---

### [ ] Phase 7: Mock Test Suite & Frontend Contract E2E Verification
- [ ] Implement end-to-end integration tests verifying frontend hook requests against Go handlers.
- [ ] Implement schema validation testing for all HTTP endpoints.
- [ ] Verify Next.js build bundle size optimization and embedded asset loading in browser.

---

### [ ] Phase 8: Embedded Hardware Bringup & Deployment Validation
- [ ] Deploy and validate single binary on physical Quectel RG501Q-EU (SDX55, Linux 4.14 kernel).
- [ ] Deploy and validate single binary on physical Quectel RM520N-GL (SDX65, Linux 5.4 kernel).
- [ ] Benchmark memory footprint (<20MB) and CPU consumption (<1% idle) under live cellular traffic.
- [ ] Fully deprecate and remove legacy `scripts/www/cgi-bin/` and shell scripts from repo.
