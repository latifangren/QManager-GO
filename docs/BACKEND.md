# QManager Backend Architecture & Implementation Guide

This document provides a comprehensive overview of the QManager standalone Go backend architecture, internal packages, direct hardware interfaces, telemetry systems, API structure, and compilation pipelines.

---

## 1. Architecture Overview

QManager's backend is compiled as a **single, static, zero-dependency Go binary** (`qmanager-armv7` / `qmanager-arm64`) targeting embedded Linux environments running on Qualcomm Snapdragon X55 (RG501Q-EU) and Snapdragon X65 (RM520N-GL) cellular modems.

### Why Go Single-Binary?
- **Zero Fork Overhead**: Replaces hundreds of shell scripts, `sed`/`awk`/`jq` invocations, and CGI sub-processes with in-memory Go execution.
- **Minimal Resource Footprint**: Idle memory footprint ~15MB RAM and near-zero idle CPU on single/dual-core ARM Cortex-A7 processors.
- **Embedded Web UI**: Embeds the static Next.js 15 frontend export (`dist/`) directly into the executable using Go's standard `embed.FS`.
- **Pure Go (`CGO_ENABLED=0`)**: Eliminates libc/musl compatibility issues across vendor Yocto/OpenWrt toolchains.
- **Elimination of Multi-Daemon Stack**: Replaces Lighttpd, Rust ping daemon (`qmanager_ping`), cron daemons, and fragmented shell watchdogs with a unified in-process supervisor.

---

## 2. Core Package Architecture

```text
backend/
├── cmd/qmanager/
│   ├── main.go               # Application entry point, daemon startup, embed.FS
│   └── dist/                 # Embedded Next.js 15 static export
└── internal/
    ├── atengine/             # AT command transport, queue, and parser
    ├── telemetry/            # Poller, ping prober, watchdog, scheduler, alerts
    ├── platform/             # Sysfs/procfs metrics, hardware detection, self-heal
    ├── config/               # Thread-safe JSON configuration store
    └── api/                  # Chi HTTP router, handlers, middlewares
```

---

### 2.1 `internal/atengine`: AT Command Engine & Transports

The `atengine` package manages all serial and shared-memory communication with the modem baseband processor.

- **`Engine` (`engine.go`)**: Thread-safe command coordinator. Serializes concurrent AT requests using `sync.Mutex` and enforces execution timeouts via Go's `context.Context`.
- **`DeviceTransport` (`transport.go`)**: Direct character device transport interfacing directly with `/dev/smd11` (or `/dev/smd7` / `/dev/ttyUSB2`) via `os.OpenFile(O_RDWR)`.
  - **Shared Memory Driver (SMD)**: Direct kernel shared memory channel without baud rate or termios configuration required.
  - **Terminator Detection**: Dynamically parses 3GPP AT response terminators (`OK`, `ERROR`, `+CME ERROR:`, `+CMS ERROR:`, `NO CARRIER`, `BUSY`).
- **`CliTransport` (`transport.go`)**: Legacy fallback for running external command-line helpers when direct character device permissions are restricted.
- **`MockTransport` (`transport.go`)**: In-memory mock implementing the `Transport` interface (`SetResponse(cmd, resp)`), enabling fast, deterministic unit testing without physical modem hardware.
- **`Parser` (`parser.go`)**: High-performance text parsing for complex AT command outputs:
  - `+QENG="servingcell"`: Serving cell parameters (RSRP, RSRQ, SINR, PCI, EARFCN/ARFCN, Band, Bandwidth, Timing Advance).
  - `+QCAINFO`: Carrier aggregation state (PCC and SCCs with bandwidths, frequencies, and channel state).
  - `+CSQ`, `+QRSRP`: Secondary and per-antenna signal power statistics.
  - `+CPIN?`, `+QNWINFO`, `+CFUN?`: SIM status, network registration, and radio power state.

---

### 2.2 `internal/telemetry`: Polling, Monitoring & Maintenance Daemons

The `telemetry` package houses background supervisor daemons running concurrently within the single Go process:

- **Signal & Stats Poller (`poller.go`)**:
  - Runs at 1 Hz in a background goroutine.
  - Periodically issues non-disruptive signal queries (`AT+QENG="servingcell"`, `AT+QCAINFO`).
  - Maintains rolling in-memory ring buffers of RSRP, RSRQ, SINR, and CA metrics for dashboard graphs.
- **Latency Prober (`ping.go`)**:
  - Performs lightweight TCP/UDP latency probes against `1.1.1.1:53` (or configured DNS target) at configurable intervals.
  - Records rolling packet loss and round-trip time (RTT) history without spawning external ping processes.
- **Connection Watchdog (`watchdog.go`)**:
  - Implements a 4-tier progressive link recovery pipeline when probes fail:
    - **Tier 1**: Soft radio restart (`AT+CFUN=0` followed by `AT+CFUN=1`).
    - **Tier 2**: Network interface and route stack restart (`systemctl restart network`).
    - **Tier 3**: Backup SIM slot switchover.
    - **Tier 4**: Gated system reboot with rate-limiting protection.
- **Monotonic Scheduler (`scheduler.go`)**:
  - Handles scheduled recurring operations (Scheduled System Reboot, Tower Lock Schedule Apply/Clear, Auto-Update Checks).
  - **1970 Clock-Step Guard (`ClockSane`, `BootSettled`)**: Protects against Qualcomm uncalibrated RTC boot-step anomalies where the clock advances from Jan 1 1970 to real time, preventing spurious reboot loops.
- **SMS Forwarder (`sms_forwarder.go`)**:
  - Background poller that checks incoming SMS inbox (`AT+CMGL="REC UNREAD"`).
  - Maintains fingerprint deduplication cache (`/tmp/qmanager_sms_seen.json`) to prevent duplicate message forwarding.
- **Alert Dispatcher (`alerts.go`)**:
  - Centralized notification dispatcher supporting multiple outgoing channels:
    - **Discord Webhook** & **Telegram Bot API**.
    - **Custom HTTP/HTTPS Webhooks** (POST with JSON payload).
    - **Email (SMTP / Gmail App Passwords)**.
    - **Cellular SMS alerts** delivered directly over the baseband control channel.
- **SSE Stream (`/api/events/stream`)**:
  - Server-Sent Events hub streaming live radio statistics and connection events directly to connected browser clients.

---

### 2.3 `internal/platform`: System Metrics, Hardware ID & Self-Heal

- **Sysfs & Procfs Reader (`sysfs.go`)**:
  - `/proc/uptime`: Monotonic system uptime parsing.
  - `/proc/meminfo`: RAM total, free, and available calculation.
  - `/proc/net/dev`: Per-interface RX/TX bytes and error counters (`rmnet_data0`, `eth0`).
  - `/sys/class/thermal/` & `/sys/class/hwmon/`: Direct modem SoC temperature reading.
- **Hardware Identity Detection (`detector.go`)**:
  - Parses `/etc/quectel-project-version` for project name (Model), revision (FW fingerprint), and SoC branch (SDX55 / SDX6X).
  - Reads `/proc/cmdline` for hardware serial number (`androidboot.serialno`).
- **Platform Profile Self-Heal (`self_heal.go`)**:
  - Manages advisory `/etc/qmanager/platform.json`.
  - **Symlink Attack & Directory Protection**: Refuses to write through symlinks or directory paths to prevent flash corruption or unauthorized file redirection.
  - **Atomic File Replacement**: Writes to a secure temporary file (`platform.json.tmp.<pid>`) and atomically renames it.

---

### 2.4 `internal/config`: Thread-Safe Configuration Store

- **Config Manager (`config.go`)**:
  - Centralized configuration stored at `/etc/qmanager/qmanager.conf`.
  - Backed by `sync.RWMutex` ensuring thread-safe reads and concurrent modifications.
  - Mutation helper `Update(func(c *Config))` ensures consistent in-memory state updates before atomic disk persistence.
  - Automatic default fallback creation matching device specifications if config is absent or corrupted.

---

### 2.5 `internal/api`: Router, Handlers & Legacy Compatibility

- **Chi Router (`router.go`)**:
  - Modular sub-routing for all modem management subsystems (`/api/cellular`, `/api/network`, `/api/monitoring`, `/api/system`, `/api/auth`).
  - **Legacy CGI Compatibility Layer**: Maps legacy URLs (`/cgi-bin/quecmanager/*`) to native Go handlers, enabling full backward compatibility with older UI scripts or external automation.
- **Authentication & Security Middleware**:
  - HTTP-only session cookie management (`qmanager_session`).
  - Constant-time password verification (`crypto/subtle`).
  - Rate limiting on authentication endpoints to defend against brute-force attacks.
- **Embedded SPA File Server**:
  - Serves static assets (`_next/`, CSS, SVG, JS) from `embed.FS`.
  - Automatic fallback to `index.html` on unmatched non-API routes to support Next.js client-side App Router navigation.

---

## 3. Compilation & Cross-Compilation

### 3.1 Build Environment
QManager compiles using standard Go cross-compilation without requiring a C cross-compiler toolchain (`CGO_ENABLED=0`).

### 3.2 Target Architectures
| Target Modem | SoC | GOOS | GOARCH | GOARM | Binary Name |
|---|---|---|---|---|---|
| Quectel RM520N-GL | Qualcomm SDX65 | `linux` | `arm` | `7` | `qmanager-armv7` |
| Quectel RG501Q-EU | Qualcomm SDX55 | `linux` | `arm` | `7` | `qmanager-armv7` |
| Generic 64-bit ARM | ARMv8 / AArch64 | `linux` | `arm64` | - | `qmanager-arm64` |

### 3.3 Makefile Commands
```bash
# Full build (Next.js export + Go ARMv7 compilation)
make build

# Cross-compile ARMv7 binary only
make build-backend

# Generate deployment archive (qmanager-armv7.tar.gz with installer)
make package

# Execute test suite
make test
```

---

## 4. Testing & Mocking Strategy

The backend includes a comprehensive test suite across all subsystems:

```bash
cd backend && go test -v -count=1 ./...
```

### Mocking Strategies
1. **AT Transport Mocking (`MockTransport`)**:
   - `MockTransport.SetResponse(cmd, resp)` simulates raw modem responses for unit testing parsers, cellular handlers, and telemetry pollers without requiring physical hardware.
2. **Scheduler & Time Mocking (`JobExecutor`)**:
   - Customizable `JobExecutor` hooks permit deterministic simulation of 1970 clock-step conditions, uptime values, and execution callbacks in `scheduler_test.go`.
3. **Filesystem Isolation (`t.TempDir()`)**:
   - Tests for `config.Manager` and `platform.SyncPlatformJSON` operate in isolated temporary directories to guarantee hermetic test execution.
