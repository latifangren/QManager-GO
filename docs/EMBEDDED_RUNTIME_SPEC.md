# Quectel RG501Q-EU Embedded Runtime Specification: Concurrency, NAND Endurance & Performance

> **Target Platform:** Quectel RG501Q-EU (Qualcomm Snapdragon X55 / Cortex-A7, Linux 4.14, Yocto, systemd, UBIFS NAND) & Quectel RM520N-GL (SDX65, Linux 5.4).  
> **Binary Architecture:** Standalone Go single binary (`qmanager-armv7`) with embedded Next.js 15 static web UI (`dist/`).

---

## 1. Concurrency & AT Engine Architecture

Direct character device `/dev/smd11` (Qualcomm Shared Memory Driver) is a single, half-duplex serial FIFO channel directly connected to the baseband processor DSP. Multiple competing threads or goroutines cannot write simultaneously without corrupting command streams or garbling AT response lines.

```text
┌─────────────────────────────────────────────────────────────────────────────┐
│                       3-Tier Priority Actor Queue                           │
│                                                                             │
│  [HIGH PRIORITY]   ──► Emergency Recovery / Watchcat / CFUN / Critical PIN  │
│  [NORMAL PRIORITY] ──► User UI Actions (APN, Band Lock, SMS Send, Settings) │
│  [LOW PRIORITY]    ──► 1 Hz Background Telemetry Poller (QENG, QCAINFO, CSQ) │
└──────────────────────────────────────┬──────────────────────────────────────┘
                                       │
                                       ▼  (Single Execution Goroutine)
                      ┌─────────────────────────────────┐
                      │    Direct Character Device      │
                      │           /dev/smd11            │
                      └────────────────┬────────────────┘
                                       │
                                       ▼
                       ┌───────────────────────────────┐
                       │  In-Memory State Broadcaster  │
                       │    & URC Demux Dispatcher     │
                       └───────────────────────────────┘
```

### 1.1 3-Tier Priority Actor Model
Instead of simple coarse mutex contention where background 1 Hz polling can stall or race against critical user requests, the AT engine operates a 3-tier priority queue:
1. **Tier 1 (High Priority)**: Emergency link recovery (Watchdog/Watchcat triggers, CFUN toggles, PIN unlock, graceful shutdown radio detach). Takes immediate precedence over all pending requests.
2. **Tier 2 (Normal Priority)**: User-initiated UI configuration requests (APN update, Band/Tower locking, SMS composition, Speedtest start, SIM profile apply).
3. **Tier 3 (Low Priority)**: Background telemetry poller (`AT+QENG="servingcell"`, `AT+QCAINFO`, `AT+CSQ`). Drops or yields if higher-tier commands are queued.

### 1.2 In-Memory State Broadcasting & URC Demuxing
- Unsolicited Result Codes (URCs) like `+CMTI` (incoming SMS notification), `+QIURC` (network socket events), and `+CEREG` (registration changes) are demuxed in real time.
- Polled signal state is held in Go memory and broadcast to SSE (`/api/events/stream`) and REST consumers, eliminating duplicate redundant AT polls from multiple web browser tabs.

---

## 2. NAND Flash Memory Protection (Zero Flash Wear Policy)

Embedded cellular modems utilize onboard SPI/raw NAND flash partitions mounted as **UBIFS**. Raw NAND has limited write-cycle endurance (typically 10,000–100,000 P/E cycles per block). Frequent disk writes (e.g. logging every second, persisting per-minute telemetry) can permanently destroy modem flash within months.

### 2.1 Golden Rule: RAM-First Storage
- **Zero Disk Writes for Telemetry & Metrics**: All signal history ring buffers, ping latency samples, network event logs, and connection statuses **MUST live exclusively in RAM** (Go runtime memory or `tmpfs` under `/tmp`).
- **RAM-Backed `/tmp`**: Runtime state flags (e.g., `/tmp/qmanager_update.pid`, `/tmp/qmanager_sms_seen.json`) live in RAM.

### 2.2 Debounced & Atomic Configuration Persistence
- Configuration files (`/etc/qmanager/qmanager.conf`, `/etc/qmanager/tower_lock.json`, `/etc/qmanager/platform.json`) are persisted to flash **only on explicit user modification**.
- **Atomic Write Sequence**:
  1. Write serialized JSON to a unique temporary file on the same filesystem: `path.tmp.<nano>`.
  2. Perform `fsync` on the file descriptor to ensure data is committed to disk blocks.
  3. Atomically `rename()` temporary file over the destination path.
  4. Explicitly reject writing through symlinks or existing directory names to prevent flash corruption or unauthorized redirection.

### 2.3 Data Usage Counter Flush Policy
- Network byte counters (`rx_bytes`, `tx_bytes`) are read directly from kernel `/proc/net/dev` without touching disk.
- Persistence across reboots is handled via a **long-interval debounce (1–6 hours)** or flushed cleanly on **SIGTERM / SIGINT shutdown traps**, ensuring zero per-second or per-minute flash wear.

---

## 3. In-Memory Ring Buffer Logging

Traditional file logging (`/var/log/qmanager.log`) creates constant write amplification on flash memory.

```text
┌────────────────────────────────────────────────────────┐
│             In-Memory Fixed Ring Buffer                │
│                                                        │
│  [ Entry 001 ] ──► [ Entry 002 ] ──► ... ──► [ Entry 1000 ]
│        ▲                                           │
│        └──────────── Overwrite Oldest ─────────────┘
└────────────────────────────────────────────────────────┘
```

- **Fixed-Capacity Circular Buffer**: Daemon logs, warnings, and error messages are written into an in-memory ring buffer (default: 500–1,000 entries).
- **Zero Disk Logging**: Standard stdout/stderr streams to `journald` in RAM-only journal mode or internal circular buffer.
- **REST Log Viewer**: `/api/system/logs` exports slices directly from the in-memory ring buffer.

---

## 4. CPU & Memory Efficiency on Single-Core Cortex-A7

The Quectel RG501Q-EU features an ARM Cortex-A7 single/dual-core CPU clocked at ~800MHz–1.2GHz with ~256MB–512MB RAM shared with the baseband DSP.

### 4.1 Low-Allocation AT Parser
- Parsers avoid excessive string allocations and heavy regular expression compilations in hot polling paths (1 Hz).
- Use index-based byte slicing (`bytes.IndexByte`, `strings.SplitN`, `bytes.Cut`), fixed-size stack buffers (`[1024]byte`), and direct ASCII integer parsing.

### 4.2 Garbage Collection (GC) & Memory Footprint Tuning
- **Bounded Target Memory**: Total daemon RSS footprint target `< 20MB`.
- **GC Profile Tuning**:
  - `GOMEMLIMIT=30MiB`: Hard ceiling preventing memory ballooning during burst requests.
  - `GOGC=50`: Aggressive GC threshold to rapidly return unused heap memory in memory-constrained embedded environments.
- **Thread Count Management**: Bound `GOMAXPROCS` to 2 to minimize goroutine scheduler thread-switching overhead on lightweight Cortex-A7 cores.
