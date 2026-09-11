# QManager-GO

<div align="center">
  <img src="frontend/public/qmanager-mark.svg" alt="QManager" width="120" />
  <h3>Modern, High-Performance Single-Binary Web & Telemetry Engine for Quectel Modems</h3>
  <p>Standalone Pure Go Engine + Embedded Next.js 16 WebUI for Quectel RM520N-GL & RG501Q-EU</p>

  ![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat-square&logo=go)
  ![Next.js](https://img.shields.io/badge/Next.js-16-black?style=flat-square&logo=next.js)
  ![Architecture](https://img.shields.io/badge/arch-ARMv7%20%7C%20ARM64%20%7C%20AMD64-blue?style=flat-square)
  ![License](https://img.shields.io/badge/license-MIT%20%2B%20Commons%20Clause-green?style=flat-square)
</div>

---

<div align="center">
  <img src="docs/screenshots/dashboard.png" alt="QManager Dashboard Screenshot" width="900" />
</div>

---

## Overview

**QManager-GO** is an all-in-one cellular management appliance and telemetry suite built for Qualcomm-based Quectel modems (including **RG501Q-EU SDX55** and **RM520N-GL SDX65**). 

Replacing legacy Lighttpd web servers, PHP-FPM, shell CGI scripts, and external helper binaries, QManager compiles down to a **single, zero-dependency standalone binary** (`qmanager-armv7`) with the optimized Next.js 16 frontend embedded directly into the Go executable via `embed.FS`.

### Key Advantages
- **Single Static Binary**: Zero external runtime requirements (no Lighttpd, PHP, Python, Entware, or Rust dependencies).
- **Direct Character Device Access**: High-performance pure-Go AT command engine interfacing directly with `/dev/smd11` with thread-safe mutex serialization and 3-tier priority execution.
- **Zero Flash Wear (RAM-First Lifecycle)**: Telemetry streams, real-time signal charts, 1000-line circular syslog buffer, and DPI proxy run exclusively in RAM/tmpfs to protect raw NAND flash (UBIFS).
- **Traffic Engine (DPI Bypass)**: Embedded on-demand `tpws` binary engine with Full Bypass, YouTube Video Optimizer, and Force TCP modes.
- **Native Web Console**: Built-in root shell terminal powered by pure Go Pseudo-Terminal (PTY) over WebSockets (`/console/ws`), eliminating external `ttyd` daemons.
- **Modular Tailscale VPN**: On-demand service management and background installer without bloating the core binary.
- **Comprehensive AT Terminal**: 2-line Command Palette with 43 Quectel diagnostic presets (temperature `AT+QTEMP`, serving/neighbour cells, Carrier Aggregation `AT+QCAINFO`, cell unlock `AT+QNWLOCK`).
- **Comprehensive Subsystem Diagnostics**: 26 native Go diagnostic probes across 8 categories with one-click `.tar.gz` Support Diagnostics Bundle export.
- **Internationalization (i18n)**: 5 languages supported (English, Bahasa Indonesia, 简体中文, 繁體中文, Italiano).

---

## Features

- **Real-Time Cellular Telemetry** — Live RSRP, RSRQ, SINR, RSSI, band, bandwidth, PCI, EARFCN/NR-ARFCN, eNodeB/gNodeB ID, and Carrier Aggregation (EN-DC + LTE-CA) topology.
- **Traffic Engine & Video Optimizer** — In-memory DPI circumvention proxy (`tpws`) with automated iptables management and live bypass verification.
- **Cell & Band Locking** — 1-click LTE/5G band selection, EARFCN/PCI locking, and emergency lock-release recovery.
- **Native Web Console** — In-browser interactive root shell terminal via PTY WebSocket bridge.
- **Tailscale VPN Integration** — Modular host daemon management with Auth URL capture and QR code login.
- **Custom DNS Presets** — 1-click apply for 9 popular public DNS providers (Cloudflare, Google, AdGuard, Quad9, Mullvad, etc.).
- **System Health Diagnostics** — Automated multi-point subsystem checks with instant support bundle generation.
- **Root SSH Management** — Secure password update engine using native `openssl passwd -1` and atomic shadow file substitution.
- **SMS Center** — In-browser SMS inbox/outbox and background forwarding engine.
- **Connection Watchdog** — 4-tier link monitoring and automatic modem recovery.

---

## Building from Source

### Prerequisites
- [Go 1.22+](https://golang.org/)
- [Bun](https://bun.sh/) (or Node.js 20+)
- `make` and standard POSIX build tools

### Build Commands

```bash
# 1. Full Build (Frontend Static Export + Go ARMv7 Binary)
make build

# 2. Build Frontend Only
make build-frontend

# 3. Build Backend Only (Cross-compilation for ARMv7 / Quectel Linux)
make build-backend

# 4. Package Release Tarball (qmanager-armv7.tar.gz)
make package

# 5. Run Backend Unit & Regression Tests
make test
```

---

## Installation & Deployment

### Quick Installation (Automated)

1. Extract the release tarball on your device:
   ```bash
   mkdir -p /tmp/qmanager_pkg && cd /tmp/qmanager_pkg
   tar -xzf qmanager-linux-armv7.tar.gz
   ```
2. Run the installer script:
   ```bash
   chmod +x install.sh
   ./install.sh
   ```
3. Open your browser at `http://192.168.225.1` (or your modem's IP).

---

### Removing Legacy QManager / SimpleAdmin (Migration)

If your modem previously ran legacy QManager (PHP/Bash/Lighttpd) or SimpleAdmin, clean it up completely:

```bash
# 1. Stop & disable legacy daemons
systemctl stop lighttpd qmanager-poller qmanager-ping qmanager-watchcat \
  qmanager-firewall qmanager-setup qmanager-cfun-fix qmanager-console \
  qmanager-ethernet qmanager-imei-check qmanager-mtu qmanager-tower-failover \
  qmanager-ttl qmanager_tailscale_install qmanager-auto-update.timer 2>/dev/null || true

systemctl disable lighttpd qmanager-poller qmanager-ping qmanager-watchcat \
  qmanager-firewall qmanager-setup qmanager-cfun-fix qmanager-console \
  qmanager-ethernet qmanager-imei-check qmanager-mtu qmanager-tower-failover \
  qmanager-ttl qmanager_tailscale_install qmanager-auto-update.timer 2>/dev/null || true

# 2. Remove legacy web server files (Preserves /etc/qmanager/ configuration!)
rm -rf /usrdata/qmanager/console \
       /usrdata/qmanager/lighttpd.conf* \
       /usrdata/qmanager/locales-* \
       /usrdata/qmanager/www \
       /usrdata/www \
       /tmp/qmanager* 2>/dev/null || true
```

---

## Troubleshooting & Documentation

- **[English Operations & Troubleshooting Guide](deploy/TROUBLESHOOTING.md)**
- **[Panduan Troubleshooting Bahasa Indonesia](deploy/TROUBLESHOOTING_ID.md)**
- **[Architecture & Developer Guide](docs/REWRITE_ARCHITECTURE.md)**

---

## Credits & Acknowledgments

This project builds upon the work of several fantastic open-source projects:

- **[dr-dolomite/QManager-RM520N](https://github.com/dr-dolomite/QManager-RM520N)** — Original QManager frontend design, UI concepts, and telemetry workflows.
- **[bol-van/zapret](https://github.com/bol-van/zapret)** by [@bol-van](https://github.com/bol-van) — High-performance, lightweight `tpws` DPI circumvention engine.
- **[tailscale/tailscale](https://github.com/tailscale/tailscale)** — Zero-config mesh VPN daemon.
- **[creack/pty](https://github.com/creack/pty)** — Pure Go Pseudo-Terminal (PTY) interface for UNIX platforms.
- **[gorilla/websocket](https://github.com/gorilla/websocket)** — Fast and reliable WebSocket implementation for Go.
- **[go-chi/chi](https://github.com/go-chi/chi)** — Lightweight, idiomatic HTTP router for Go.

---

## License

This project is licensed under the [MIT License with Commons Clause](LICENSE).  
Personal, educational, and non-commercial use is permitted.
