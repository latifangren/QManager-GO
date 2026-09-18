#!/bin/sh
# =============================================================================
# QManager Go Single-Binary Installer for Quectel Modems (RM520N / RG501Q)
# =============================================================================
set -e

INSTALL_DIR="$(cd "$(dirname "$0")" && pwd)"
BIN_SRC="$INSTALL_DIR/qmanager"
if [ ! -f "$BIN_SRC" ]; then
    BIN_SRC="$INSTALL_DIR/qmanager-armv7"
fi

DEST_DIR="/usrdata/qmanager"
CONF_DIR="/etc/qmanager"
SYSTEMD_SYSTEM="/lib/systemd/system"
[ ! -d "$SYSTEMD_SYSTEM" ] && SYSTEMD_SYSTEM="/etc/systemd/system"
SERVICE_SRC="$INSTALL_DIR/deploy/systemd/qmanager.service"
if [ ! -f "$SERVICE_SRC" ]; then
    SERVICE_SRC="$INSTALL_DIR/qmanager.service"
fi

# Check for uninstall flag
if [ "$1" = "--uninstall" ] || [ "$1" = "-u" ] || [ "$1" = "uninstall" ]; then
    echo "======================================================"
    echo " 🛑 Uninstalling QManager Go Single-Binary..."
    echo "======================================================"

    echo "==> Stopping and disabling qmanager service..."
    systemctl stop qmanager 2>/dev/null || true
    systemctl disable qmanager 2>/dev/null || true

    echo "==> Removing systemd service file..."
    rm -f "$SYSTEMD_SYSTEM/qmanager.service" 2>/dev/null || true
    rm -f "/etc/systemd/system/qmanager.service" 2>/dev/null || true
    systemctl daemon-reload 2>/dev/null || true

    echo "==> Removing binary from $DEST_DIR/qmanager..."
    rm -f "$DEST_DIR/qmanager" 2>/dev/null || true

    echo "==> Cleaning up runtime temp files..."
    rm -f /tmp/qmanager* 2>/dev/null || true

    echo "======================================================"
    echo " ✅ QManager has been successfully uninstalled."
    echo " (Configuration files under $CONF_DIR preserved)."
    echo "======================================================"
    exit 0
fi

echo "======================================================"
echo " 🚀 Installing QManager Go Single-Binary..."
echo "======================================================"

# 1. Platform Detection
MODEL="Unknown"
SOC="Unknown"
if [ -f /etc/quectel-project-version ]; then
    MODEL=$(grep -i "Project Name" /etc/quectel-project-version | awk -F: '{print $2}' | tr -d ' \r\n' || true)
    SOC=$(grep -i "Branch Name" /etc/quectel-project-version | awk -F: '{print $2}' | tr -d ' \r\n' || true)
fi
ARCH=$(uname -m 2>/dev/null || echo "armv7l")
echo "📦 Detected Platform: Model=${MODEL}, SoC=${SOC}, Arch=${ARCH}"

# 2. Stop legacy web servers & daemons if running
echo "==> Stopping legacy services (lighttpd, dropbear, bash pollers, previous qmanager)..."
systemctl stop bridge-eth0 dnsmasq 2>/dev/null || true
systemctl disable bridge-eth0 2>/dev/null || true
systemctl stop lighttpd dropbear qmanager-poller qmanager-ping qmanager-watchcat \
    qmanager-firewall qmanager-setup qmanager-cfun-fix qmanager-console \
    qmanager-ethernet qmanager-imei-check qmanager-mtu qmanager-tower-failover \
    qmanager-ttl qmanager_tailscale_install qmanager-auto-update.timer 2>/dev/null || true

systemctl disable lighttpd dropbear qmanager-poller qmanager-ping qmanager-watchcat \
    qmanager-firewall qmanager-setup qmanager-cfun-fix qmanager-console \
    qmanager-ethernet qmanager-imei-check qmanager-mtu qmanager-tower-failover \
    qmanager-ttl qmanager_tailscale_install qmanager-auto-update.timer 2>/dev/null || true

systemctl stop qmanager 2>/dev/null || true
killall -9 qmanager dropbear lighttpd qmanager_poller qmanager_ping ttyd 2>/dev/null || true

# Purge legacy systemd units (ensure Quectel rootfs is rw)
mount -o remount,rw / 2>/dev/null || true
rm -f /lib/systemd/system/sysinit.target.wants/dropbear.service \
      /etc/systemd/system/multi-user.target.wants/dropbear.service \
      /lib/systemd/system/dropbear.service \
      /etc/systemd/system/dropbear.service \
      /lib/systemd/system/multi-user.target.wants/qmanager-* \
      /lib/systemd/system/multi-user.target.wants/lighttpd.service \
      /etc/systemd/system/multi-user.target.wants/qmanager-* \
      /etc/systemd/system/multi-user.target.wants/lighttpd.service 2>/dev/null || true
rm -f /lib/systemd/system/qmanager-* /lib/systemd/system/lighttpd.service 2>/dev/null || true
rm -f /etc/systemd/system/qmanager-* /etc/systemd/system/lighttpd.service 2>/dev/null || true
rm -f /lib/systemd/system/bridge-eth0.service /etc/systemd/system/bridge-eth0.service /lib/systemd/system/dnsmasq.service /etc/systemd/system/dnsmasq.service 2>/dev/null || true
rm -f /opt/etc/init.d/S80lighttpd 2>/dev/null || true
rm -rf /opt/etc/lighttpd 2>/dev/null || true
rm -f /usr/bin/qmanager_* 2>/dev/null || true
rm -rf /usrdata/qmanager/console /usrdata/qmanager/lighttpd.conf* /usrdata/qmanager/locales-* /usrdata/qmanager/www /usrdata/qmanager/data_used.json /usrdata/www /www 2>/dev/null || true
systemctl daemon-reload 2>/dev/null || true

# 3. Prepare directories & config
echo "==> Preparing directories..."
mkdir -p "$DEST_DIR"
mkdir -p "$CONF_DIR"
chmod 0755 "$DEST_DIR"
chmod 0755 "$CONF_DIR"

# 4. Install binary
echo "==> Installing binary to $DEST_DIR/qmanager..."
if [ ! -f "$BIN_SRC" ]; then
    echo "❌ Error: qmanager binary not found at $BIN_SRC" >&2
    exit 1
fi

cp -f "$BIN_SRC" "$DEST_DIR/qmanager"
chmod 0755 "$DEST_DIR/qmanager"

# 5. Install systemd unit
echo "==> Installing systemd service..."
if [ -f "$SERVICE_SRC" ]; then
    cp -f "$SERVICE_SRC" "$SYSTEMD_SYSTEM/qmanager.service"
    chmod 0644 "$SYSTEMD_SYSTEM/qmanager.service"
else
    echo "==> Creating default systemd service at $SYSTEMD_SYSTEM/qmanager.service..."
    cat << 'EOF' > "$SYSTEMD_SYSTEM/qmanager.service"
[Unit]
Description=QManager Single-Binary Web & Telemetry Daemon
After=basic.target
Wants=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/usrdata/qmanager
ExecStart=/usrdata/qmanager/qmanager
Restart=always
RestartSec=3s
LimitNOFILE=65535

# Cortex-A7 Runtime & Memory Protection
Environment="GOMEMLIMIT=30MiB"
Environment="GOGC=50"

# In-memory logging to journald / RAM ring buffer
StandardOutput=journal
StandardError=journal

# Graceful termination
TimeoutStopSec=10s
KillMode=mixed

[Install]
WantedBy=multi-user.target
EOF
    chmod 0644 "$SYSTEMD_SYSTEM/qmanager.service"
fi

# Ensure cold-boot autostart symlink in /lib (Quectel rootfs authority)
mkdir -p "$SYSTEMD_SYSTEM/multi-user.target.wants"
ln -sf "$SYSTEMD_SYSTEM/qmanager.service" "$SYSTEMD_SYSTEM/multi-user.target.wants/qmanager.service"

# 6. Enable and start unit
echo "==> Reloading systemd and enabling service..."
systemctl daemon-reload
systemctl enable qmanager 2>/dev/null || true
systemctl restart qmanager

# Determine primary IP
IP_ADDR=$(ip -4 addr show bridge0 2>/dev/null | grep -o 'inet [0-9.]*' | cut -d ' ' -f 2 || true)
if [ -z "$IP_ADDR" ]; then
    IP_ADDR=$(ip -4 addr show eth0 2>/dev/null | grep -o 'inet [0-9.]*' | cut -d ' ' -f 2 || true)
fi
if [ -z "$IP_ADDR" ]; then
    IP_ADDR="192.168.225.1"
fi

echo "======================================================"
echo " ✅ QManager Go Single-Binary successfully installed!"
echo " Web UI running at http://${IP_ADDR}/"
echo " View live logs: journalctl -u qmanager -f"
echo "======================================================"
