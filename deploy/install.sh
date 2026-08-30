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

echo "======================================================"
echo " 🚀 Installing QManager Go Single-Binary..."
echo "======================================================"

# 1. Stop legacy web servers & daemons if running
echo "==> Stopping legacy services (lighttpd, previous qmanager)..."
systemctl stop lighttpd 2>/dev/null || true
systemctl disable lighttpd 2>/dev/null || true
systemctl stop qmanager 2>/dev/null || true
killall -9 qmanager 2>/dev/null || true
killall -9 lighttpd 2>/dev/null || true

# 2. Prepare directories & config
echo "==> Preparing directories..."
mkdir -p "$DEST_DIR"
mkdir -p "$CONF_DIR"
chmod 0755 "$DEST_DIR"
chmod 0755 "$CONF_DIR"

# 3. Install binary
echo "==> Installing binary to $DEST_DIR/qmanager..."
if [ ! -f "$BIN_SRC" ]; then
    echo "❌ Error: qmanager binary not found at $BIN_SRC" >&2
    exit 1
fi

cp -f "$BIN_SRC" "$DEST_DIR/qmanager"
chmod 0755 "$DEST_DIR/qmanager"

# 4. Install systemd unit
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
StandardOutput=journal
StandardError=journal
TimeoutStopSec=10s
KillMode=mixed

[Install]
WantedBy=multi-user.target
EOF
    chmod 0644 "$SYSTEMD_SYSTEM/qmanager.service"
fi

# 5. Enable and start unit
echo "==> Reloading systemd and enabling service..."
systemctl daemon-reload
systemctl enable qmanager
systemctl restart qmanager

echo "======================================================"
echo " ✅ QManager Go Single-Binary successfully installed!"
echo " Web UI running on port 8080 (or system port)."
echo " Check logs: journalctl -u qmanager -f"
echo "======================================================"
