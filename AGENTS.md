# QManager AI Agent Instructions & Architectural Standards

> **Target Device:** Quectel RG501Q-EU (Qualcomm Snapdragon X55, Cortex-A7, Linux 4.14, Yocto, systemd, UBIFS NAND) & Quectel RM520N-GL (Qualcomm SDX65, Cortex-A7, Linux 5.4).  
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

### 1.3 3-Tier Priority AT Command Execution & SMD11 Debounce
- Access to `/dev/smd11` is half-duplex and serialized.
- Priority order:
  1. **High Priority**: Emergency watchdog recovery, radio restart (`CFUN`), PIN operations.
  2. **Normal Priority**: User UI configuration actions (APN, Band Lock, SMS, Settings).
  3. **Low Priority**: 1 Hz background signal poller (`+QENG`, `+QCAINFO`, `+CSQ`).
- Low-priority polling must yield or drop if higher-priority operations are queued.
- **SMD11 Debounce & Inter-Command Delay**: Maintain an inter-command delay (20ms–50ms) between consecutive AT commands in the actor loop to give Qualcomm baseband firmware time to flush SMD shared memory buffers.
- **Boot Warmup & Readiness Probe**: After modem boot / daemon start, execute readiness probe (`AT` -> `OK`) with bounded retry before spawning high-rate background telemetry pollers to prevent flooding an uninitialized baseband.

### 1.4 CPU & Memory Efficiency on Cortex-A7
- Target memory RSS: `< 20MB`.
- Keep parsers zero- or low-allocation: prefer byte scanning (`bytes.IndexByte`, `strings.SplitN`, `bytes.Cut`) over heavy regular expression matching on 1 Hz hot paths.
- Default runtime flags: `CGO_ENABLED=0`, `GOMEMLIMIT=30MiB`, `GOGC=50`.

### 1.5 AT Command Timeout & Context Cancellation
- Every AT command execution to `/dev/smd11` MUST be bound by `context.WithTimeout` (default 5s for fast queries, 30s for cell/band scans, 60s for radio restart).
- No blocking unbounded I/O on serial or character device file descriptors.

### 1.6 1970 System Clock-Step Safety
- Modems often boot with default hardware RTC timestamp `1970-01-01` before NITZ/NTP synchronization occurs.
- Watchdogs, schedulers, and token expirations MUST NOT trigger premature failover actions or reboots until system clock validity is verified (e.g. `time.Now().Year() >= 2024`).

### 1.7 Strict Zero-Allocation on 1 Hz Hot Paths
- Telemetry poller runs at 1 Hz on single-core Cortex-A7.
- Avoid dynamic heap allocations, regex recompilation, or heavy string concatenation inside the polling cycle.

### 1.8 API Envelope & CGI Parity Contract
- All modified or newly introduced endpoints MUST maintain dual API compatibility: REST (`/api/v1/*`) and backward-compatible CGI shims (`/cgi-bin/quecmanager/*`) to guarantee compatibility with legacy scripts and the Next.js frontend.

---

## 2. No Hardcoding & Configuration Injection Policy
- **NO HARDCODED PATHS OR PARAMETERS**: Never hardcode file paths, listening ports, serial device paths, URLs, timeouts, or magic numbers directly in business logic when they can be configured or injected.
- **Dependency Injection & Options**: All handlers and subsystem managers must accept their storage paths, dependencies, and configuration directories dynamically (e.g., via struct constructors, `AppServices.ConfigDir`, functional options).
- **Environment & CLI Hierarchy**: Support overrides via CLI flags and environment variables with safe defaults (e.g., `-port` / `PORT` / `QM_PORT`, `-device` / `AT_DEVICE` / `QM_AT_DEVICE`, `-config-dir` / `QM_CONFIG_DIR`).
- **Test Isolation**: Tests must never write to system root paths (`/etc/qmanager/`, `/tmp/`); always use isolated temporary directories via `t.TempDir()`.

---

## 3. Git Operations & Workflow Discipline
- **EXPLICIT USER APPROVAL FOR COMMITS & PUSHES**:
  - **NEVER** run `git commit`, `git push`, `git merge`, `git rebase`, or create Git tags automatically unless explicitly instructed by the user in the prompt.
  - **NEVER** amend, force-push (`--force`), or delete branches without explicit user permission.
  - Before committing (when requested), inspect `git status` and `git diff` carefully to ensure only intended changes are staged and no secrets, temporary debug files, or unwanted build artifacts are included.
- **Verification First**: Always run the test suite (`go test ./...`) and cross-compilation check before proposing or executing a commit.

---

## 4. Codebase Organization

- `frontend/`: Next.js 15 App Router, React 19, Tailwind CSS v4, shadcn/ui. Static export target is `frontend/out/`.
- `backend/`: Pure-Go daemon (`qmanager/cmd/qmanager`). Embeds `dist/` via Go standard `//go:embed dist/*`.
  - `internal/atengine/`: Direct character device transport (`/dev/smd11`), 3GPP AT parser, mock transport, inter-command debounce.
  - `internal/telemetry/`: 1 Hz signal poller, latency prober, 4-tier watchdog, 1970 clock-step guarded scheduler, SMS forwarder.
  - `internal/platform/`: Hardware detector, sysfs metrics, platform profile self-heal, firewall auto-init.
  - `internal/config/`: Thread-safe `/etc/qmanager/qmanager.conf` manager.
  - `internal/api/`: Chi HTTP router, session authentication, domain REST handlers, and backward-compatible CGI routes.
- `deploy/`: Systemd service unit (`qmanager.service`) and standalone device installer (`install.sh`).
- `Makefile`: Central cross-compilation pipeline (`make build`, `make package`, `make test`).
