# AT Command Transport Architecture

> **Applies to:** RM520N-GL (SDX65) and RG501Q-EU (SDX55) · Go Single-Binary Architecture

---

## 1. Overview

In QManager's single-binary architecture, all AT command communication is handled natively in Go by `backend/internal/atengine`. 

External C/Rust binaries (`atcli_smd11`) and shell-based filesystem locks (`/tmp/qmanager_at.lock` with `flock`) have been completely superseded by direct character device access (`DeviceTransport`) and in-memory Go concurrency synchronization (`sync.Mutex`).

---

## 2. Direct Character Device Transport (`DeviceTransport`)

The primary transport on Quectel Linux is `DeviceTransport`, which opens the Qualcomm Shared Memory Driver (SMD) device directly:

```go
f, err := os.OpenFile(devPath, os.O_RDWR, 0)
```

### 2.1 Qualcomm SMD Characteristics
- **No Baud Rate / Termios Needed**: Unlike legacy USB virtual serial ports (`/dev/ttyUSB2`), Qualcomm `/dev/smd11` and `/dev/smd7` are kernel-level shared-memory FIFO queues directly connected to the baseband modem DSP.
- **Direct Read/Write**: Commands are written with standard `\r\n` line terminations and responses are read directly into an in-memory buffer until a 3GPP terminator is encountered.
- **Zero Process Overhead**: Avoids forking external sub-processes for every AT command query.

### 2.2 Response Framing & 3GPP Terminators
The Go transport reads the stream until matching standard 3GPP AT terminators:
- `\r\nOK\r\n` / `\nOK\n`
- `\r\nERROR\r\n` / `\nERROR\n`
- `+CME ERROR: <err>`
- `+CMS ERROR: <err>`
- `\r\nNO CARRIER\r\n`
- `\r\nBUSY\r\n`

A 100ms per-chunk read deadline combined with caller context timeouts (`ctx.Done()`) guarantees that unprompted baseband stalls never hang the web server or polling routines.

---

## 3. Transport Autodetection Priority

The `AutoDetectTransport()` function automatically selects the optimal transport at runtime:

```text
1. /dev/smd11     ──> Direct SMD11 Character Device (RM520N / RG501Q default)
2. /dev/smd7      ──> Direct SMD7 Character Device (Secondary SMD channel)
3. /dev/ttyUSB2   ──> USB Serial AT Port (External/USB host fallback)
4. atcli_smd11    ──> External CLI binary helper fallback
5. MockTransport  ──> In-memory mock for local development and CI unit tests
```

---

## 4. In-Memory Concurrency & Serialization

### 4.1 Previous Architecture vs New Architecture

| Dimension | Legacy CGI Architecture | Go Single-Binary Architecture |
|---|---|---|
| **Locking Mechanism** | Filesystem lock (`flock /tmp/qmanager_at.lock`) | In-memory `sync.Mutex` in `atengine.Engine` |
| **Lock Contention** | High fork overhead; risk of stale `/tmp` lock files | Microsecond lock acquisition in Go runtime |
| **Deadlock Recovery** | External watchdog scripts clearing lock files | Native `context.WithTimeout()` per command |
| **Transport** | Repeated process execution of `atcli_smd11` | Direct open handle to `/dev/smd11` |

### 4.2 Engine Mutex Synchronization
`Engine.ExecContext(ctx, cmd)` acquires a mutual exclusion lock before writing to the transport:
1. Locks `Engine.mu`.
2. Sends the command via the active `Transport` with context deadline.
3. Parses output into a structured `*Result` object.
4. Releases `Engine.mu`.

This guarantees that high-frequency background polling (1 Hz telemetry, cell scanning, SMS checks) never interleaves with user-initiated configuration changes (band locking, APN updates, tower locks).
