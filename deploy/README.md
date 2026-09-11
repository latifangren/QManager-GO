# QManager-GO (Single-Binary Release)

Next-generation high-performance management appliance and telemetry web suite for Quectel 5G/LTE modems, written in Go with embedded Next.js 16 WebUI.

---

## 📦 Package Contents
- `qmanager`: Standalone single-binary executable for your target architecture (ARMv7 / ARM64 / AMD64).
- `qmanager.service`: Systemd service unit definition with memory caps and auto-restart policy.
- `install.sh`: Automated one-step installer and uninstaller script.
- `README.md`: This deployment and operations guide.

---

## 🚀 Quick Installation (Automated)

### Method A: Via SSH or Terminal (Recommended)
1. Extract the release tarball to temporary folder on your device:
   ```sh
   mkdir -p /tmp/qmanager_pkg && cd /tmp/qmanager_pkg
   tar -xzf qmanager-linux-armv7.tar.gz
   ```
2. Run the installer:
   ```sh
   chmod +x install.sh
   ./install.sh
   ```
3. Open your web browser and navigate to:
   `http://192.168.225.1` (or your modem's configured gateway IP).

---

### Method B: Via ADB (From PC / Host)
1. Push and install directly via ADB:
   ```sh
   adb push qmanager-armv7 /usrdata/qmanager/qmanager
   adb push qmanager.service /lib/systemd/system/qmanager.service
   adb shell "chmod +x /usrdata/qmanager/qmanager && systemctl daemon-reload && systemctl restart qmanager"
   ```

---

## 🛠 Manual Installation
If you prefer manual setup:
```sh
# 1. Stop existing service
systemctl stop qmanager 2>/dev/null || true

# 2. Copy binary
mkdir -p /usrdata/qmanager
cp qmanager /usrdata/qmanager/qmanager
chmod +x /usrdata/qmanager/qmanager

# 3. Setup systemd service
cp qmanager.service /lib/systemd/system/qmanager.service
systemctl daemon-reload
systemctl enable qmanager
systemctl start qmanager

# 4. Check status
systemctl status qmanager
```

---

## 🗑 Uninstallation
To uninstall QManager and remove systemd service:
```sh
./install.sh --uninstall
```

---

## 📋 System Requirements
- **Target Modems**: Quectel RG501Q-EU (Qualcomm SDX55), Quectel RM520N-GL (Qualcomm SDX65), and compatible Linux Yocto / OpenWrt ARMv7 & ARM64 appliances.
- **Kernel**: Linux 4.14+ / 5.4+.
- **RAM**: Minimal memory footprint (~18-25 MB RAM under load).
- **Storage**: ~18 MB persistent storage at `/usrdata/qmanager/`.

---

## 🤝 Upstream & Credits
- **Project Repository**: [latifangren/QManager-GO](https://github.com/latifangren/QManager-GO)
- **Upstream UI Foundation**: [dr-dolomite/QManager-RM520N](https://github.com/dr-dolomite/QManager-RM520N)
