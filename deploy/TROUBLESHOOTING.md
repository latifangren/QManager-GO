# QManager-GO Troubleshooting & Operations Guide

A comprehensive technical reference and debugging guide for **QManager-GO** on Quectel 5G/LTE modems (Quectel RG501Q-EU SDX55, RM520N-GL SDX65, and compatible Linux Yocto / OpenWrt appliances).

---

## 📑 Table of Contents
1. [Service Architecture & Startup Diagnostics](#1-service-architecture--startup-diagnostics)
2. [AT Engine & Serial Communication (/dev/smd11)](#2-at-engine--serial-communication-devsmd11)
3. [Traffic Engine & DPI Bypass (tpws)](#3-traffic-engine--dpi-bypass-tpws)
4. [Native Web Console (PTY WebSocket)](#4-native-web-console-pty-websocket)
5. [Tailscale VPN (On-Demand Module)](#5-tailscale-vpn-on-demand-module)
6. [SSH Access & System Passwords](#6-ssh-access--system-passwords)
7. [Cellular Diagnostics, Band Lock & Cell Lock](#7-cellular-diagnostics-band-lock--cell-lock)
8. [Storage Safety, RAM-First Policy & Cleanups](#8-storage-safety-ram-first-policy--cleanups)
9. [Support Bundle & Bug Reporting](#9-support-bundle--bug-reporting)

---

## 1. Service Architecture & Startup Diagnostics

QManager-GO runs as a standalone, single-binary Go executable (`/usrdata/qmanager/qmanager`) managed by systemd.

### 🔹 Checking Service Status
```sh
systemctl status qmanager
```

### 🔹 Checking Real-Time Logs
```sh
# View systemd journal logs
journalctl -u qmanager -f -n 50

# Or query the in-memory RAM ring buffer via API
curl -s http://127.0.0.1/api/v1/system/logs | jq .
```

### 🔹 Manual Foreground Debugging
If the service fails to start or crashes immediately, stop systemd and run the binary in foreground to view all panic/error traces:
```sh
systemctl stop qmanager
PORT=80 GOMEMLIMIT=30MiB /usrdata/qmanager/qmanager
```

### 🔹 Common Startup Issues & Fixes
* **Address already in use (Port 80 conflict):**
  Check if a legacy webserver (e.g. `lighttpd`, `nginx`, `uhttpd`) is still running:
  ```sh
  killall lighttpd nginx uhttpd 2>/dev/null || true
  systemctl stop lighttpd 2>/dev/null || true
  systemctl disable lighttpd 2>/dev/null || true
  systemctl restart qmanager
  ```
* **Permission denied on binary:**
  ```sh
  chmod +x /usrdata/qmanager/qmanager
  ```

---

## 2. AT Engine & Serial Communication (/dev/smd11)

QManager communicates with the Qualcomm baseband processor through `/dev/smd11` using a prioritized, half-duplex thread-safe engine.

### 🔹 AT Engine Priorities:
1. **High Priority (Priority 0):** Watchdog checks, Emergency Recovery, Radio Reboot (`CFUN=1,1`).
2. **Normal Priority (Priority 1):** User actions (Band Lock, APN change, SMS, manual AT commands).
3. **Low Priority (Priority 2):** 1-Hz background telemetry polling (`+QENG`, `+QCAINFO`, `+CSQ`).

### 🔹 Symptoms of AT Engine Failure:
* WebUI displays *"Modem busy"* or dashboard signal cards show *"No signal / Offline"*.
* AT Terminal commands time out after 5000ms.

### 🔹 Fixes:
1. Verify `/dev/smd11` device node exists:
   ```sh
   ls -la /dev/smd11
   ```
2. Test basic AT communication from shell:
   ```sh
   echo -e "AT\r\n" > /dev/smd11 && cat < /dev/smd11
   ```
3. If `/dev/smd11` is locked by a zombie process:
   ```sh
   fuser /dev/smd11
   # Restart QManager daemon to reset the serial descriptor
   systemctl restart qmanager
   ```

---

## 3. Traffic Engine & DPI Bypass (tpws)

The Traffic Engine uses an embedded `tpws` (zapret v72.13 ARMv7) binary extracted dynamically into `/tmp/tpws` in RAM.

### 🔹 Verification Steps:
1. Check if `tpws` process is active:
   ```sh
   pidof tpws
   ps | grep tpws
   ```
2. Inspect iptables NAT redirection rules:
   ```sh
   iptables -t nat -L PREROUTING -n -v
   # Should show redirect to port 989 for TCP 80/443
   ```
3. Test DPI Bypass directly from the WebUI:
   Navigate to **Networking > Traffic Engine** and click **Verify Bypass Status**.

### 🔹 Troubleshooting:
* **Internet connection slows down or drops after enabling:**
  * Try switching mode from **Full Bypass** to **Video Optimizer (YouTube DPI Fix)** or vice versa.
  * Check MTU settings: ensure WAN/WWAN MTU is at least 1420-1500.
* **Firewall rules not applying:**
  * Verify kernel has `iptable_nat` and `xt_REDIRECT` support:
    ```sh
    iptables -t nat -L -n
    ```

---

## 4. Native Web Console (PTY WebSocket)

The Web Console runs a native Go Pseudo-Terminal (PTY) bridged to WebSocket at `/console/ws`.

### 🔹 Symptoms & Fixes:
* **Error `Close code 1006` or `The console service isn't running`:**
  * Ensure QManager is running the latest single-binary Go build.
  * Verify WebSocket connection using Python / curl:
    ```sh
    curl -i -N -H "Connection: Upgrade" -H "Upgrade: websocket" http://127.0.0.1/console/ws
    ```
  * Note: Legacy `ttyd` daemon and `lighttpd.conf` proxies are obsolete and no longer needed.

---

## 5. Tailscale VPN (On-Demand Module)

Tailscale runs modularly to prevent NAND flash bloat.

### 🔹 Managing Service:
```sh
# Check status
tailscale status
systemctl status tailscaled

# Start / Restart
systemctl restart tailscaled

# Reset state and get login link
tailscale up --reset
```

### 🔹 Manual / Offline Installation:
If the modem has no internet during initial setup, extract `tailscale` and `tailscaled` ARMv7 binaries directly to `/usrdata/tailscale/`:
```sh
mkdir -p /usrdata/tailscale
cp tailscale tailscaled /usrdata/tailscale/
chmod +x /usrdata/tailscale/*
ln -sf /usrdata/tailscale/tailscale /usr/bin/tailscale
```

---

## 6. SSH Access & System Passwords

Quectel Linux Yocto modems lack GNU `chpasswd` / `usermod`. QManager-GO uses `/usr/bin/openssl passwd -1` and atomic file substitution on `/etc/shadow`.

### 🔹 Manual Password Recovery (via ADB or Web Console):
If root SSH password becomes out of sync:
```sh
# Generate hash for new password (e.g. 'root123')
NEW_HASH=$(openssl passwd -1 "root123")

# Set in /etc/shadow for root user
sed -i "s|^root:[^:]*|root:${NEW_HASH}|" /etc/shadow
```

---

## 7. Cellular Diagnostics, Band Lock & Cell Lock

### 🔹 Releasing Stuck Cell Lock:
If you locked a PCI/cell that is currently offline or unreachable, the modem will lose connectivity.
Run the following unlock commands in **System Settings > AT Terminal**:
```text
# Unlock LTE (4G) cell lock:
AT+QNWLOCK="common/4g",0

# Unlock 5G NR cell lock:
AT+QNWLOCK="common/5g",0

# Restart radio:
AT+CFUN=1,1
```

### 🔹 Resetting Network Mode Preference:
```text
# Reset to Auto (5G NR + LTE):
AT+QNWPREFCFG="mode_pref",AUTO

# Enable both SA and NSA modes:
AT+QNWPREFCFG="nr5g_disable_mode",0
```

---

## 8. Storage Safety, RAM-First Policy & Cleanups

### 🔹 Flash Wear Protection
All continuous metrics (ping probes, signal chart series, syslog buffers) are kept exclusively in RAM (`tmpfs` / `/tmp`). Persistent writes to `/etc/qmanager/` occur only when settings are modified by user.

### 🔹 Cleaning Legacy Files (Freeing Space on `/usrdata`):
```sh
rm -rf /usrdata/qmanager/console \
       /usrdata/qmanager/lighttpd.conf* \
       /usrdata/qmanager/locales-* \
       /usrdata/qmanager/www \
       /usrdata/www \
       /tmp/qmanager* 2>/dev/null || true
```

---

## 9. Support Bundle & Bug Reporting

1. Navigate to **System Settings > Health Check** in the WebUI.
2. Wait for all 26 subsystem probes to pass.
3. Click **Download Support Bundle** to obtain `qmanager-support-bundle-*.tar.gz`.
4. Attach the bundle to an issue on GitHub:
   👉 **[https://github.com/latifangren/QManager-GO/issues](https://github.com/latifangren/QManager-GO/issues)**
