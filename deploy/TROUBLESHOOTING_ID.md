# Panduan Troubleshooting & Pemecahan Masalah QManager-GO

Panduan referensi teknis dan penanganan masalah (*troubleshooting*) untuk **QManager-GO** pada modem Quectel 5G/LTE (Quectel RG501Q-EU SDX55, RM520N-GL SDX65, serta perangkat Linux Yocto / OpenWrt yang kompatibel).

---

## 📑 Daftar Isi
1. [Arsitektur Service & Diagnostik Startup](#1-arsitektur-service--diagnostik-startup)
2. [AT Engine & Komunikasi Serial (/dev/smd11)](#2-at-engine--komunikasi-serial-devsmd11)
3. [Traffic Engine & Bypass DPI (tpws)](#3-traffic-engine--bypass-dpi-tpws)
4. [Web Console Native (PTY WebSocket)](#4-web-console-native-pty-websocket)
5. [Tailscale VPN (Modul On-Demand)](#5-tailscale-vpn-modul-on-demand)
6. [Akses SSH & Manajemen Password](#6-akses-ssh--manajemen-password)
7. [Diagnostik Sinyal Seluler, Band Lock & Cell Lock](#7-diagnostik-sinyal-seluler-band-lock--cell-lock)
8. [Keamanan Flash NAND, Kebijakan RAM-First & Pembersihan](#8-keamanan-flash-nand-kebijakan-ram-first--pembersihan)
9. [Support Diagnostics Bundle & Laporan Bug](#9-support-diagnostics-bundle--laporan-bug)

---

## 1. Arsitektur Service & Diagnostik Startup

QManager-GO berjalan sebagai *single-binary* Go mandiri (`/usrdata/qmanager/qmanager`) yang dikelola oleh systemd.

### 🔹 Memeriksa Status Service
```sh
systemctl status qmanager
```

### 🔹 Memeriksa Log Real-Time
```sh
# Melihat log journal systemd
journalctl -u qmanager -f -n 50

# Atau melihat log ring buffer di RAM via API
curl -s http://127.0.0.1/api/v1/system/logs | jq .
```

### 🔹 Debugging Manual di Foreground
Jika service gagal berjalan atau crash saat startup, matikan systemd service dan jalankan binary langsung di terminal foreground untuk melihat stack trace:
```sh
systemctl stop qmanager
PORT=80 GOMEMLIMIT=30MiB /usrdata/qmanager/qmanager
```

### 🔹 Masalah Umum & Solusi Startup
* **Address already in use (Port 80 Konflik):**
  Pastikan webserver lama (seperti `lighttpd`, `nginx`, atau `uhttpd`) sudah dimatikan:
  ```sh
  killall lighttpd nginx uhttpd 2>/dev/null || true
  systemctl stop lighttpd 2>/dev/null || true
  systemctl disable lighttpd 2>/dev/null || true
  systemctl restart qmanager
  ```
* **Permission Denied pada Binary:**
  ```sh
  chmod +x /usrdata/qmanager/qmanager
  ```

---

## 2. AT Engine & Komunikasi Serial (/dev/smd11)

QManager berkomunikasi langsung dengan prosesor baseband Qualcomm melalui serial `/dev/smd11` dengan sistem antrean berprioritas (*thread-safe*).

### 🔹 Tingkat Prioritas AT Engine:
1. **Prioritas Tinggi (Priority 0):** Watchdog recovery, Reset Radio / Modem Reboot (`CFUN=1,1`).
2. **Prioritas Normal (Priority 1):** Aksi user di WebUI (Band Lock, ganti APN, SMS, kirim AT manual).
3. **Prioritas Rendah (Priority 2):** Polling sinyal background 1-Hz (`+QENG`, `+QCAINFO`, `+CSQ`).

### 🔹 Gejala Masalah AT Engine:
* WebUI menampilkan pesan *"Modem busy"* atau indikator sinyal menunjukkan *"No signal / Offline"*.
* Eksekusi AT Terminal mengalami timeout lebih dari 5000ms.

### 🔹 Cara Penanganan:
1. Periksa apakah device node `/dev/smd11` tersedia:
   ```sh
   ls -la /dev/smd11
   ```
2. Uji komunikasi AT dasar dari terminal:
   ```sh
   echo -e "AT\r\n" > /dev/smd11 && cat < /dev/smd11
   ```
3. Jika `/dev/smd11` terkunci oleh proses yang hang:
   ```sh
   fuser /dev/smd11
   # Restart daemon QManager untuk mereset file descriptor serial
   systemctl restart qmanager
   ```

---

## 3. Traffic Engine & Bypass DPI (tpws)

Traffic Engine menggunakan binary `tpws` (zapret v72.13 ARMv7) yang di-embed langsung dan diekstrak ke RAM (`/tmp/tpws`) saat diaktifkan.

### 🔹 Langkah Verifikasi:
1. Cek apakah proses `tpws` berjalan:
   ```sh
   pidof tpws
   ps | grep tpws
   ```
2. Periksa rule redirect firewall iptables:
   ```sh
   iptables -t nat -L PREROUTING -n -v
   # Harus menampilkan aturan redirect TCP 80/443 ke port 989
   ```
3. Uji status bypass langsung dari WebUI:
   Buka menu **Networking > Traffic Engine** dan klik tombol **Verify Bypass Status**.

### 🔹 Pemecahan Masalah:
* **Koneksi internet lambat / drop setelah diaktifkan:**
  * Coba ubah mode dari **Full Bypass** ke **Video Optimizer (YouTube DPI Fix)** atau sebaliknya.
  * Periksa nilai MTU: pastikan MTU WAN/WWAN berada pada kisaran 1420–1500.
* **Firewall rules gagal diterapkan:**
  * Pastikan modul kernel `iptable_nat` dan `xt_REDIRECT` aktif:
    ```sh
    iptables -t nat -L -n
    ```

---

## 4. Web Console Native (PTY WebSocket)

Fitur Web Console menggunakan jembatan native Pseudo-Terminal (PTY) Go yang terhubung ke WebSocket pada endpoint `/console/ws`.

### 🔹 Gejala & Solusi:
* **Error `Close code 1006` atau `The console service isn't running`:**
  * Pastikan modem telah menjalankan binary Go QManager-GO terbaru.
  * Uji koneksi WebSocket dari shell:
    ```sh
    curl -i -N -H "Connection: Upgrade" -H "Upgrade: websocket" http://127.0.0.1/console/ws
    ```
  * Catatan: Binary `ttyd` lama dan konfigurasi proxy `lighttpd` sudah tidak diperlukan lagi.

---

## 5. Tailscale VPN (Modul On-Demand)

Tailscale dikelola secara on-demand agar tidak memakan ruang memori flash secara permanen jika tidak digunakan.

### 🔹 Mengelola Service:
```sh
# Cek status login & peers
tailscale status
systemctl status tailscaled

# Restart service
systemctl restart tailscaled

# Reset autentikasi & buat link login baru
tailscale up --reset
```

### 🔹 Instalasi Manual / Offline:
Jika modem belum memiliki akses internet saat instalasi pertama kali, ekstrak binary `tailscale` dan `tailscaled` ARMv7 langsung ke `/usrdata/tailscale/`:
```sh
mkdir -p /usrdata/tailscale
cp tailscale tailscaled /usrdata/tailscale/
chmod +x /usrdata/tailscale/*
ln -sf /usrdata/tailscale/tailscale /usr/bin/tailscale
```

---

## 6. Akses SSH & Manajemen Password

QManager-GO telah mengintegrasikan **Native Go Standalone SSH Server** langsung di dalam binary utama. Anda tidak memerlukan Dropbear dari Entware.

### 🔹 Fitur & Konfigurasi SSH:
- **Port:** Default port `22` (dapat diubah melalui WebUI `System Settings > SSH Access`).
- **Autentikasi:** Membaca password root secara dinamis dari `/etc/shadow` (MD5 crypt `$1$`).
- **Authorized Keys:** Mendukung login tanpa password melalui file `/etc/qmanager/ssh/authorized_keys` atau via input WebUI.
- **Host Key:** Auto-generate Ed25519 host key di `/etc/qmanager/ssh/id_ed25519`.

### 🔹 Deteksi Konflik Port SSH (Dropbear Lama):
Jika modem sebelumnya terpasang Dropbear manual dan berjalan di port yang sama, WebUI akan menampilkan banner peringatan **`PORT CONFLICT`**. Untuk mengatasinya:
```sh
# Matikan dan nonaktifkan dropbear lama
systemctl stop dropbear 2>/dev/null || true
systemctl disable dropbear 2>/dev/null || true
killall -9 dropbear 2>/dev/null || true
rm -f /lib/systemd/system/sysinit.target.wants/dropbear.service
# Restart qmanager untuk mengambil alih port
systemctl restart qmanager
```

### 🔹 Pemulihan Password Root Manual:
Jika password SSH root mengalami kendala autentikasi:
```sh
# Buat hash password baru (misalnya password 'root123')
NEW_HASH=$(openssl passwd -1 "root123")

# Tulis langsung ke /etc/shadow untuk user root
sed -i "s|^root:[^:]*|root:${NEW_HASH}|" /etc/shadow
```

---

## 7. Self-Healing LAN & Gateway Provisioning

QManager-GO memiliki modul *Zero-Touch Network Provisioner* yang memastikan port LAN Ethernet (`eth0`) dan DHCP server (`dnsmasq`) selalu siap pakai tanpa perlu konfigurasi manual setelah modem di-reset.

### 🔹 Fitur Jaringan Otomatis:
- **Bridge Otomatis:** Membuat `bridge0` dan mendaftarkan `eth0` sebagai anggota.
- **Anti Link-Local Collision:** Otomatis membersihkan IP liar `169.254.x.x` dari `eth0`.
- **DHCP Server Auto-Config:** Menulis `/etc/dnsmasq.conf` dan memastikan service `dnsmasq` aktif membagikan IP (`192.168.225.20 - 192.168.225.100`).
- **Dual HTTP & HTTPS:** Port 80 dan 443 aktif bersamaan dengan auto self-signed certificate ECDSA P-256 (`https://192.168.225.1`).

### 🔹 Troubleshooting Jaringan LAN:
Jika PC tidak mendapatkan IP dari kabel LAN:
```sh
# 1. Periksa interface bridge0 dan IP
ip addr show bridge0

# 2. Periksa status DHCP server (dnsmasq)
systemctl status dnsmasq

# 3. Restart manual jika diperlukan
systemctl restart dnsmasq
```

---

## 7. Diagnostik Sinyal Seluler, Band Lock & Cell Lock

### 🔹 Membuka Kuncian Cell Lock yang Tersangkut:
Jika Anda mengunci modem ke tower/PCI yang tiba-tiba mati atau tidak terjangkau, sinyal modem akan hilang (*No Service*).
Jalankan perintah berikut di menu **System Settings > AT Terminal**:
```text
# Buka kunci cell LTE (4G):
AT+QNWLOCK="common/4g",0

# Buka kunci cell 5G NR:
AT+QNWLOCK="common/5g",0

# Restart radio seluler:
AT+CFUN=1,1
```

### 🔹 Mereset Preferensi Mode Jaringan:
```text
# Reset mode jaringan ke Otomatis (5G NR + LTE):
AT+QNWPREFCFG="mode_pref",AUTO

# Aktifkan mode SA dan NSA sekaligus:
AT+QNWPREFCFG="nr5g_disable_mode",0
```

---

## 8. Keamanan Flash NAND, Kebijakan RAM-First & Pembersihan

### 🔹 Pencegahan Flash Wear (Ketahanan Memori)
Seluruh metrik polling, data grafik sinyal, log ring buffer, dan cache latency berjalan di RAM (`tmpfs` / `/tmp`). Penulisan permanen ke `/etc/qmanager/` hanya terjadi saat user menekan tombol simpan perubahan.

### 🔹 Membersihkan Sisa File WebUI Legacy (Hemat Ruang `/usrdata`):
```sh
rm -rf /usrdata/qmanager/console \
       /usrdata/qmanager/lighttpd.conf* \
       /usrdata/qmanager/locales-* \
       /usrdata/qmanager/www \
       /usrdata/www \
       /tmp/qmanager* 2>/dev/null || true
```

---

## 9. Support Diagnostics Bundle & Laporan Bug

1. Buka menu **System Settings > Health Check** di WebUI.
2. Tunggu hingga 26 probe diagnostik selesai diuji.
3. Klik tombol **Download Support Bundle** untuk mengunduh arsip `qmanager-support-bundle-*.tar.gz`.
4. Unggah file diagnostik tersebut saat membuat issue di GitHub:
   👉 **[https://github.com/latifangren/QManager-GO/issues](https://github.com/latifangren/QManager-GO/issues)**
