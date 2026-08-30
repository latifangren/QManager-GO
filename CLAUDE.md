# CLAUDE.md — Working Agreement & Core Technical Standards

> **Target Platform:** Quectel RG501Q-EU (Qualcomm Snapdragon X55, Cortex-A7, Linux 4.14, Yocto, systemd, UBIFS NAND) & Quectel RM520N-GL (SDX65, Linux 5.4).  
> **Appliance Architecture:** Single standalone Go binary (`qmanager-armv7`) embedding the Next.js 15 static export (`dist/`).

---

## 1. Golden Rules of Embedded Engineering & NAND Flash Protection

### 1.1 Zero Flash Wear Policy (RAM-First)
- **NEVER write continuous telemetry, metrics, ping results, or signal history to disk.**
- All chart series, event logs, latency history, and polling buffers **MUST remain in memory (RAM)** or `tmpfs` (`/tmp`).
- Raw NAND flash (UBIFS) has limited write cycle endurance. Writing every second or every minute will brick the modem flash memory.
- Configuration persistence (`/etc/qmanager/`) must only occur on explicit user-triggered mutation.
- All persistent disk writes MUST use atomic temporary-file-and-rename:
  1. Write to unique temporary file: `filepath.tmp.<nano>`
  2. Perform `fsync` on the file descriptor.
  3. Rename over the target file path (`os.Rename`).
  4. Reject writing through symlinks or directories.

### 1.2 In-Memory Ring Buffer Logging
- The daemon must never log continuously to flash storage (`/var/log` or persistent disks).
- Maintain an in-memory fixed circular ring buffer (500–1000 lines) for daemon logs and operational events.
- `/api/system/logs` reads directly from the RAM ring buffer.

### 1.3 3-Tier Priority AT Command Execution
- Access to `/dev/smd11` is half-duplex and serialized.
- Priority order:
  1. **High Priority**: Emergency watchdog recovery, radio restart (`CFUN`), PIN operations.
  2. **Normal Priority**: User UI configuration actions (APN, Band Lock, SMS, Settings).
  3. **Low Priority**: 1 Hz background signal poller (`+QENG`, `+QCAINFO`, `+CSQ`).
- Low-priority polling must yield or drop if higher-priority operations are queued.

### 1.4 CPU & Memory Efficiency on Cortex-A7
- Target memory RSS: `< 20MB`.
- Keep parsers zero- or low-allocation: prefer byte scanning (`bytes.IndexByte`, `strings.SplitN`, `bytes.Cut`) over heavy regular expression matching on 1 Hz hot paths.
- Default runtime flags: `CGO_ENABLED=0`, `GOMEMLIMIT=30MiB`, `GOGC=50`.

---

## 2. Codebase Organization

- `frontend/`: Next.js 15 App Router, React 19, Tailwind CSS v4, shadcn/ui. Static export target is `frontend/out/`.
- `backend/`: Pure-Go daemon (`qmanager/cmd/qmanager`). Embeds `dist/` via Go standard `//go:embed dist/*`.
  - `internal/atengine/`: Direct character device transport (`/dev/smd11`), 3GPP AT parser, mock transport.
  - `internal/telemetry/`: 1 Hz signal poller, latency prober, 4-tier watchdog, 1970 clock-step guarded scheduler, SMS forwarder.
  - `internal/platform/`: Hardware detector, sysfs metrics, platform profile self-heal.
  - `internal/config/`: Thread-safe `/etc/qmanager/qmanager.conf` manager.
  - `internal/api/`: Chi HTTP router, session authentication, domain REST handlers, and backward-compatible CGI routes.
- `deploy/`: Systemd service unit (`qmanager.service`) and standalone device installer (`install.sh`).
- `Makefile`: Central cross-compilation pipeline (`make build`, `make package`, `make test`).
