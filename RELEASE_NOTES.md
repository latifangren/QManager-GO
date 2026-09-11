# QManager-GO v1.0.0-beta Release Notes

Welcome to the **v1.0.0-beta** release of **QManager-GO**! 🎉

This release marks a complete architectural overhaul of the QManager modem appliance management suite. The entire legacy stack (Lighttpd, PHP, bash CGI wrappers, and external daemons) has been replaced by a **single, unified, standalone Go binary** (`qmanager-armv7`) with an embedded Next.js 16 WebUI.

---

## 🌟 Major Highlights

### ⚡ Single-Binary Pure Go Architecture
- Replaces legacy multi-component dependencies (Lighttpd, PHP-FPM, shell CGI scripts, ttyd) with a lightweight, high-performance Go binary.
- Dramatically lowers idle resource consumption (~18–25 MB RAM total) with ultra-fast sub-millisecond API response times.
- Simple one-file deployment to `/usrdata/qmanager/qmanager`.

### 🛡 Zero Flash Wear (RAM-First Lifecycle)
- Designed specifically for Qualcomm NAND flash memory (UBIFS) safety on Quectel RG501Q-EU and RM520N-GL modems.
- Telemetry streams, real-time signal metrics, circular syslog buffers (1000 lines), latency benchmarks, and DPI traffic proxies operate strictly in RAM (`/tmp` / tmpfs).
- Persistent storage writes occur solely on explicit user configuration changes via atomic temporary-file-and-rename semantics.

### 🚀 Traffic Engine (DPI Bypass & Video Optimizer)
- Embedded in-memory `tpws` binary engine with safe automated lifecycle management.
- Supports **Full Bypass Mode**, **Video Optimizer (YouTube DPI Fix)**, and **Force TCP Mode**.
- Automated iptables redirection and packet mangling without interfering with modem routing rules.
- Built-in real-time DPI verification tester.

### 💻 Native Web Console (PTY Terminal)
- Built-in root shell terminal accessible directly within the browser.
- Uses native Go Pseudo-Terminal (PTY) over WebSockets (`/console/ws`), completely eliminating the need for external `ttyd` daemons.
- Full xterm.js compatibility with keyboard navigation, copy/paste, ANSI color rendering, and dynamic window resizing.

### 📡 AT Command Terminal & Smart Command Palette
- Redesigned 2-line responsive command popover palette.
- **43 Pre-configured Quectel Diagnostic Presets** categorized into 8 functional groups:
  - *Modem & Hardware*: Temperature monitoring (`AT+QTEMP`), firmware revision (`AT+QGMR`), model identification (`ATI`).
  - *Network & Registration*: Mode preference, SA/NSA toggling, operator registration (`AT+COPS?`, `AT+CEREG?`, `AT+C5GREG?`).
  - *Signal & RF Quality*: Detailed serving cell metrics (`AT+QENG="servingcell"`), neighbour cells, Carrier Aggregation (`AT+QCAINFO`), signal strength (`AT+CSQ`).
  - *SIM Management*: Dual SIM switching (`AT+QUIMSLOT`), ICCID query (`AT+CCID`), PIN status (`AT+CPIN?`).
  - *APN & Data*: APN profile manager, PDP context IP address (`AT+CGPADDR=1`).
  - *Band Locking & Cell Lock*: LTE/5G band query, cell lock configuration and reset (`AT+QNWLOCK`).
  - *IP Passthrough & USB*: USB network composition mode (`AT+QCFG="usbnet"`), MPDN passthrough.

### 🔒 On-Demand Tailscale VPN
- Modular host management for `tailscaled` service.
- Automatic background installer downloads official ARMv7 binaries on demand without bloating the main QManager binary.
- Complete lifecycle controls: Connect (with browser Auth URL / QR code), Disconnect, Logout, Boot Persistence, and Tailscale SSH toggle.

### 🩺 Comprehensive Subsystem Diagnostics (Health Check)
- 26 native Go diagnostic probes across 8 critical subsystem categories:
  - System Core, Cellular Radio, AT Engine, Network & Routing, SIM & Security, Storage & Flash, Hardware & Thermal, Telemetry.
- One-click **Support Diagnostics Bundle** export (`.tar.gz`) for streamlined bug reporting.

### 🌐 Custom DNS & Public Presets
- 1-click selector for 9 popular secure DNS providers (Cloudflare, Google, AdGuard, Quad9, Mullvad, OpenDNS, Control D, NextDNS, Level3).
- Full custom IPv4/IPv6 manual DNS entry support.

### 🔐 Root SSH Password Management
- Secure password updater using embedded `openssl passwd -1` (MD5-crypt) and atomic `/etc/shadow` rewrite.
- Verified for Linux Yocto environments lacking legacy `chpasswd`/`usermod` utilities.

### 🌍 Internationalization (i18n)
- Comprehensive multi-language translations across 5 languages:
  - English, Bahasa Indonesia, 简体中文, 繁體中文, Italiano.

---

## 📦 Release Artifacts
- `qmanager-linux-armv7.tar.gz`: Pre-packaged bundle for Quectel RG501Q-EU (SDX55) and RM520N-GL (SDX65).
- `qmanager-linux-arm64.tar.gz`: Pre-packaged bundle for AArch64 appliances and modems.
- `qmanager-linux-amd64.tar.gz`: Pre-packaged bundle for x86_64 development and testing.
- `SHA256SUMS.txt`: SHA256 integrity checksums for all release binaries.

---

## 🤝 Upstream & Credits
- **Main Repository**: [latifangren/QManager-GO](https://github.com/latifangren/QManager-GO)
- **Frontend UI Foundation**: [dr-dolomite/QManager-RM520N](https://github.com/dr-dolomite/QManager-RM520N)
