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
echo "==> Stopping legacy services (lighttpd, previous qmanager)..."
systemctl stop lighttpd 2>/dev/null || true
systemctl disable lighttpd 2>/dev/null || true
systemctl stop qmanager 2>/dev/null || true
killall -9 qmanager 2>/dev/null || true
killall -9 lighttpd 2>/dev/null || true

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
After=network.target local-fs.target
Wants=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/usrdata/qmanager
ExecStart=/usrdata/qmanager/qmanager
Restart=always
RestartSec=5s
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

# 6. Enable and start unit
echo "==> Reloading systemd and enabling service..."
systemctl daemon-reload
systemctl enable qmanager
systemctl restart qmanager

# Determine primary IP
IP_ADDR=$(ip -4 addr show rmnet_data0 2>/dev/null | grep -o 'inet [0-9.]*' | cut -d ' ' -f 2 || true)
if [ -z "$IP_ADDR" ]; then
    IP_ADDR=$(ip -4 addr show eth0 2>/dev/null | grep -o 'inet [0-9.]*' | cut -d ' ' -f 2 || true)
fi
if [ -z "$IP_ADDR" ]; then
    IP_ADDR="192.168.225.1"
fi

echo "======================================================"
echo " ✅ QManager Go Single-Binary successfully installed!"
echo " Web UI running at http://${IP_ADDR}:8080 or http://192.168.225.1:8080"
echo " View live logs: journalctl -u qmanager -f"
echo "======================================================"
