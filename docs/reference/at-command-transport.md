# AT Command Transport Architecture & Performance Specification

> **Target Hardware:** Quectel RG501Q-EU (Qualcomm Snapdragon X55, Cortex-A7 @ 1.0 GHz, Linux 4.14, UBIFS)  
> **Target Hardware:** Quectel RM520N-GL (Qualcomm Snapdragon X65, Cortex-A7 @ 1.0 GHz, Linux 5.4, UBIFS)  
> **Target Hardware:** Quectel RM551E-GL (Qualcomm Snapdragon X72/X75, Cortex-A53 ARMv8, Linux 5.15)  
> **Module:** `backend/internal/atengine`  
> **Reference Implementation:** [`1alessandro1/atcli_rust`](https://github.com/1alessandro1/atcli_rust)

---

## 1. Executive Summary & Problem Statement

On embedded cellular routers powered by single-core Qualcomm processors (Cortex-A7 @ 1.0 GHz), AT command polling is the primary heartbeat of the device. High-cadence telemetry loops (1–2 seconds) execute 6 to 8 AT commands per iteration (`+QENG`, `+QRSRP`, `+QCAINFO`, `+QTEMP`, `+CSQ`, `+QNWINFO`, `+COPS`).

### Historical Pitfalls & Evolution:
1. **Legacy Shell Wrapper (`/usr/bin/qcmd`):**
   - Each command spawned `/bin/sh`, invoking nested subshells, `flock`, `tr`, `grep`, `sed`, and `awk`.
   - **Performance Impact:** Produced **67+ new process forks per second**. The kernel spent **75%–88% of CPU time in `%sys` mode**, causing total CPU saturation (0% idle, load average > 4.5), thermal throttling, and UI latency spikes.
2. **Standard Go `os.OpenFile` on `/dev/smd11` (Runtime Netpoller Bug):**
   - Standard Go `os.OpenFile` registers file descriptors to the Go runtime `epoll` netpoller and sets `O_NONBLOCK`.
   - The Qualcomm Shared Memory Driver (`smd_pkt` / `/dev/smd11`) **does not implement standard epoll/non-blocking polling**, causing standard Go reads/writes to fail immediately with `EBUSY` ("device or resource busy") or hang.
3. **The Solution — Multi-Tier Direct Transport:**
   - **Tier 1 (Primary):** Pure Go in-process Raw POSIX Syscalls (`syscall.Open`, `syscall.Write`, `syscall.Select`, `syscall.Read`) bypassing Go's netpoller.
   - **Tier 2 (Embedded Fail-safe):** Statically linked `atcli-rs` ARM binary (`backend/internal/atengine/embeds/atcli_smd11`) extracted to `/usr/bin/atcli_smd11` once during bootstrap.

---

## 2. Benchmark & Performance Comparison

Measurements taken on **Quectel RG501Q-EU (Qualcomm SDX55 Single-Core Cortex-A7 @ 1.0 GHz)** during active 1s telemetry polling:

| Metric | Legacy Shell (`qcmd`) | Native Binary (`atcli_smd11`) | Native Go Syscall (`atengine`) |
| :--- | :--- | :--- | :--- |
| **Kernel Process Forks** | `67 forks/sec` (202 / 3s) | `7 forks/sec` (1 per cmd) | **`0 forks/sec` (0% fork overhead)** |
| **CPU %sys (Kernel Overhead)** | `75.0% – 88.0%` | `10.0% – 16.0%` | **`8.0% – 11.0%`** |
| **CPU Idle %** | `0.0% – 10.0%` (Throttling) | `75.0% – 82.0%` | **`83.0% – 91.0%`** |
| **QManager Daemon CPU** | N/A (Subshell bound) | `~4.0% – 7.0%` | **`3.5% – 5.5%`** |
| **Per-Command Latency** | `~120ms – 250ms` | `~60ms – 75ms` | **`~15ms – 40ms`** |
| **Memory / Flash Wear** | High (Shell sub-processes) | Zero (RAM execution) | **Zero (In-process memory)** |

---

## 3. Qualcomm SMD Channels & Hardware Routing

Qualcomm SDX modem platforms expose internal hardware shared memory FIFO channels mapped to character devices:

```
+-------------------------------------------------------------------------------+
|                             Qualcomm SDX Baseband DSP                         |
+------------------------------------+------------------------------------------+
                                     |
                 +-------------------+-------------------+
                 |                                       |
                 v                                       v
         [ /dev/smd11 ]                               [ /dev/smd7 ]
         (Local AT Channel)                       (USB Bridged AT Channel)
                 |                                       |
                 v                                       v
      +---------------------+                 +---------------------+
      |  QManager Go Daemon |                 | /usr/bin/port_bridge|
      |   (Local Web / API) |                 |    smd7 at_usb2 1   |
      +---------------------+                 +----------+----------+
                                                         |
                                                         v
                                                [ /dev/ttyUSB2 ]
                                                (External PC / USB)
```

### ⚠️ Critical Hardware Rule: `/dev/smd7` vs `/dev/smd11`
- **`/dev/smd11` (DO USE):** The official dedicated local AT channel for on-device applications and scripts.
- **`/dev/smd7` (NEVER LOCK OR ACCESS DIRECTLY):** `/dev/smd7` is permanently bound to the Qualcomm system daemon `/usr/bin/port_bridge smd7 at_usb2 1`. If QManager opens or locks `/dev/smd7`, the USB bridge crashes, terminating external PC communication (`AT port /dev/ttyUSB2` / QPST / QXDM).

---

## 4. Implementation Details: Native POSIX Syscall Transport

To achieve zero-fork in-process communication with `/dev/smd11` without triggering Go netpoller `EBUSY` bugs, `backend/internal/atengine` uses raw POSIX system calls.

### 4.1 Safe File Locking (`flock`)
Modem AT ports are shared with external system utilities (e.g. `sms_tool`, Quectel thermal daemons). QManager synchronizes hardware access via `/tmp/qmanager_at.lock` using POSIX `flock(LOCK_EX)`:

```go
func acquireFileLock(lockPath string) (*os.File, error) {
    f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0666)
    if err != nil {
        return nil, err
    }
    if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
        _ = f.Close()
        return nil, err
    }
    return f, nil
}
```

### 4.2 Raw File Descriptor I/O & `syscall.Select`
1. Open character device directly with `syscall.Open(devPath, syscall.O_RDWR, 0)`.
2. Write command string ending with `\r\n`.
3. Use `syscall.Select` with `syscall.Timeval` (100ms timeout per slice) to wait for readable data on the raw file descriptor.
4. Read incoming bytes into a slice and evaluate 3GPP result terminators.

```go
func readDeviceRawResponse(ctx context.Context, fd int) (string, error) {
    var out bytes.Buffer
    buf := make([]byte, 1024)
    deadline, hasDeadline := ctx.Deadline()

    for {
        if hasDeadline && time.Now().After(deadline) {
            return out.String(), ErrTimeout
        }

        var readSet syscall.FdSet
        fdSet(&readSet, fd)
        tv := syscall.Timeval{Sec: 0, Usec: 100000} // 100ms select slice

        nEvents, err := syscall.Select(fd+1, &readSet, nil, nil, &tv)
        if err != nil {
            if errors.Is(err, syscall.EINTR) {
                continue
            }
            return out.String(), err
        }
        if nEvents == 0 {
            if terminated, _ := evaluateResponseTerminator(out.String()); terminated {
                return out.String(), nil
            }
            continue
        }

        n, readErr := syscall.Read(fd, buf)
        if n > 0 {
            out.Write(buf[:n])
            if terminated, termErr := evaluateResponseTerminator(out.String()); terminated {
                return out.String(), termErr
            }
        }
    }
}
```

---

## 5. 3GPP AT Response Terminators

The parser detects completion when any of the following standard 3GPP terminators appear at the end of a line:
- `OK`
- `ERROR`
- `+CME ERROR: <code/string>`
- `+CMS ERROR: <code/string>`
- `NO CARRIER`
- `BUSY`
- `NO ANSWER`
- `NO DIALTONE`
- `CONNECT` / `CONNECT <baud>`
- `RING`

---

## 6. Multi-Tier Fail-Safe Fallback Mechanism

`DeviceTransport` implements automatic transparent fallback:

```
                         +-----------------------------------+
                         |       AT Command Execution        |
                         +-----------------+-----------------+
                                           |
                                           v
                       +---------------------------------------+
                       | Tier 1: Pure-Go Raw Syscall (/dev/smd11)|
                       +-------------------+-------------------+
                                           |
                              [ Success? ]-+-[ Yes ]--> Return Output
                                           |
                                        [ No ]
                                           |
                                           v
                       +---------------------------------------+
                       | Tier 2: atcli_smd11 (Rust Binary)     |
                       | (Restored via EnsureAtcliBinary())    |
                       +-------------------+-------------------+
                                           |
                              [ Success? ]-+-[ Yes ]--> Return Output
                                           |
                                        [ No ]
                                           |
                                           v
                       +---------------------------------------+
                       | Tier 3: Shell Fallback (/usr/bin/qcmd)|
                       +---------------------------------------+
```

### Automatic Binary Self-Restoration:
The static Rust binary `atcli_smd11` is embedded into the Go binary (`//go:embed embeds/atcli_smd11`). If `/usr/bin/atcli_smd11` is accidentally deleted or running on a fresh rootfs without dependencies, `EnsureAtcliBinary()` writes the embedded binary to `/usr/bin/atcli_smd11` (or `/usrdata/bin/atcli_smd11`) with executable permissions `0755` on startup.

---

## 7. Acknowledgment & Provenance

The standalone binary fallback is powered by:
* **[1alessandro1/atcli_rust](https://github.com/1alessandro1/atcli_rust)** by [@1alessandro1](https://github.com/1alessandro1)  
  *Safe, lightweight AT Command CLI utility written in Rust, reverse-engineered for Qualcomm SDX55/SDX65/SDX75 modems.*
