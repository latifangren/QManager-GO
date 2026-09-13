# QManager-GO v1.1.0-beta Release Notes

Welcome to the **v1.1.0-beta** release of **QManager-GO**! 🎉

This release delivers major performance breakthroughs, native POSIX syscall AT transport, full embedded binary self-healing, real-time vnStat bandwidth telemetry with SSE streaming, and core security hardening.

---

## 🌟 Major Highlights & New Features

### ⚡ Direct POSIX Syscall AT Transport (Zero-Fork In-Process Engine)
* **Eliminated Kernel Fork Overhead:** Replaced legacy shell wrappers (`/usr/bin/qcmd`) and high-frequency child processes with an in-process native Go AT engine communicating directly via POSIX raw syscalls (`syscall.Open`, `syscall.Write`, `syscall.Select`) over `/dev/smd11`.
* **Massive CPU Load Reduction:** Reduced kernel process fork rate from **67 forks/sec down to 1 fork/sec**. Modem CPU idle increased from ~10–15% up to **80%–85% idle** (total system load dropped to ~15%, QManager process consuming only 4%–9% CPU).
* **Multi-Tier Fallback & Self-Healing:** Integrated 3-tier transport mechanism:
  1. *Tier 1 (Primary):* Native Go POSIX Syscalls over `/dev/smd11`.
  2. *Tier 2 (Fail-safe):* Embedded native `atcli_smd11` (Rust utility from `1alessandro1/atcli_rust`).
  3. *Tier 3 (Legacy):* Shell-based invocation fallback.
* **Auto-Restoration:** If `atcli_smd11` or `sms_tool` is missing from the modem filesystem (e.g. after a factory reset), the Go binary automatically restores them from embedded memory on boot.

### 📊 Real-Time vnStat Bandwidth Monitoring & Telemetry SSE Stream
* **Live Bandwidth Metrics:** Real-time upload/download bitrate (bps/Kbps/Mbps) and traffic counters queried via kernel `/proc/net/dev` and `vnstat` integration.
* **Server-Sent Events (SSE):** Added lightweight streaming endpoint (`/api/v1/telemetry/stream` & `/api/v1/monitoring/bandwidth`) for live reactive frontend updates without heavy HTTP request polling.

### 📱 Robust SMS Engine & UCS-2 / PDU Reassembly
* **Embedded `sms_tool`:** Statically embedded `sms_tool` binary with automatic extraction on clean rootfs.
* **UCS-2 / UTF-16 Hex Fallback Parser:** Added automatic hex-to-UTF-8 decoding for UCS-2 formatted incoming SMS strings (`00530065...` $\rightarrow$ "Selamat...").
* **Multipart SMS Concatenation:** Correctly parses and stitches multi-segment SMS messages seamlessly in WebUI.

### ⏱ Dynamic Adaptive Polling Synchronization
* **Two-Way Polling Cadence Sync:** Added `/api/v1/system/polling` and `/cgi-bin/quecmanager/system/polling.sh` endpoints.
* Frontend power modes (`Active: 1s`, `Balanced: 2s`, `Low Power: 5s`) dynamically adjust the backend telemetry poller timer on the fly to conserve CPU cycles when WebUI is backgrounded.

### 🚀 Native Pure Go Speedtest Engine (Zero External Ookla Binary)
* **Zero CLI Dependency:** Replaced external proprietary Ookla binary (`speedtest-cli`) execution with a 100% native Go speedtest engine (`github.com/showwin/speedtest-go`).
* **RAM-First In-Memory Progress:** Ping, latency jitter, download/upload streams, and final results are handled directly in RAM matching exact frontend contract schemas without persistent disk writes.
* **Instant Availability:** Speed test is always available out-of-the-box regardless of whether external binary downloads succeeded or failed during installation.

### 🛡️ Standalone Native Go SSH Server (Zero Entware Dependency)
* **Pure Go SSH Server Daemon:** Built-in standalone SSH server listening on Port 22 (or user-defined custom port) using `golang.org/x/crypto/ssh` and `creack/pty`.
* **Dynamic Shadow Authentication:** Direct authentication against Linux `/etc/shadow` (MD5 crypt `$1$`) with zero CGO dependencies.
* **Authorized Keys Support:** Passwordless public key login via `/etc/qmanager/ssh/authorized_keys` with direct paste support in WebUI.
* **Port Conflict Protection:** Automatic detection of conflicting legacy services (Dropbear/OpenSSH) with UI warning badges and installer auto-purge.

### 🔒 Zero-Touch Auto TLS / HTTPS Support
* **Dual Port Listener:** Simultaneous support for HTTP (`:80`) and HTTPS (`:443`).
* **On-the-Fly Self-Signed Certificates:** Pure Go ECDSA P-256 TLS certificate generator (`internal/tlsgen`) ensuring instant zero-config HTTPS encryption on first boot.

### 🌐 Self-Healing Zero-Touch LAN & Gateway Provisioning
* **Automatic PCIe Ethernet & Bridge Configuration:** Automatically configures `bridge0`, attaches PCIe Ethernet (`eth0`), and flushes rogue link-local addresses (`169.254.x.x`).
* **Dynamic DHCP Daemon:** Automatically generates `/etc/dnsmasq.conf` matching the configured gateway subnet and manages `dnsmasq.service`.
* **WWAN Backhaul Auto-Fix:** Automatically patches `mobileap_cfg.xml` to ensure cellular mobile data flows cleanly to Ethernet LAN clients.

### 🔒 Security Hardening & Session Authentication
* **Web Console PTY Auth:** Enforced strict session authentication token checks on WebSocket console connections (`/console/ws`).
* **Cross-Platform Build Tagging:** Hardened OS/architecture build constraints for POSIX locking and terminal controls.
* **API Response Contract Alignment:** Normalized JSON response envelopes across `/api/v1/cellular/ping-profile`, `/api/v1/cellular/quality-thresholds`, and system telemetry endpoints.

---

## 📋 Full Changelog (Since v1.0.0-beta)

* `42ee6250` - **feat(sms):** embed `sms_tool` with auto-restoration and update transport docs
* `9352ae7c` - **feat(telemetry):** optimize atengine with native syscall transport and adaptive polling sync
* `ede6d5e5` - **feat(monitoring):** add vnstat bandwidth monitoring and sse telemetry stream
* `48daea78` - **fix(handlers):** align ping-profile and quality-thresholds responses with frontend contract
* `0f8682db` - **fix(frontend):** calculate data staleness using unix timestamp in seconds
* `99f08ac9` - **perf(core):** optimize single-core CPU usage across backend and frontend
* `2f64b2ec` - **test(backend):** boost test coverage to 83.9% and harden logs/dpi handlers
* `3028a604` - **fix(security):** enforce session authentication on web console and fix cross-platform build tags

---

## 📦 Deployment & Installation

### Automated Install via SSH/ADB:
```sh
mkdir -p /tmp/qmanager_pkg && cd /tmp/qmanager_pkg
tar -xzf qmanager-armv7.tar.gz
chmod +x install.sh
./install.sh
```

### Verified Compatible Hardware:
* **Quectel RG501Q-EU** (Qualcomm Snapdragon X55, Cortex-A7 @ 1.0 GHz, Linux 4.14 Yocto)
* **Quectel RM520N-GL** (Qualcomm Snapdragon X65, Cortex-A7 @ 1.5 GHz, Linux 5.4 Yocto)
* **Quectel RM551E-GL** (Qualcomm Snapdragon X72/X75 ARM64/ARMv8)
