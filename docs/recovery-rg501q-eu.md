# 🛠️ Panduan Recovery Total Pasca Factory Reset (0): Setup ADB, SSH, Realtek RTL8125 LAN & Internet Gateway

> ⚠️ **CATATAN PENTING & DISCLAIMER (TESTED ON QUECTEL RG501Q-EU):**
> Seluruh prosedur, nilai parameter AT command, driver Ethernet Realtek (`r8125`), susunan USB composition (`0x2C7C, 0x0801`), serta algoritma salt MD5 QADBKEY (`SH_adb_quectel`) pada dokumentasi ini **telah diuji dan divalidasi secara langsung pada modem Quectel RG501Q-EU** (Qualcomm Snapdragon X55 / arsitektur ARMv7 32-bit `sdxprairie` Yocto Linux).
>
> ⚠️ **Perbedaan pada Modem Lain:**
> Modem Quectel tipe lain (seperti RM520N / SDX62, RM551E / SDX75, RM500Q, RG650V, dll.) memiliki perbedaan arsitektur CPU (ARMv7 vs ARM64), PHY/Ethernet controller hardware (Realtek vs Aquantia vs Intel/Marvell), susunan partisi rootfs, serta USB VID/PID bawaan. **Jangan menerapkan perintah driver hardware dan unlock ini ke modem seri lain tanpa penyesuaian spesifikasi.**

---

## 📋 Ikhtisar Masalah Pasca `ResetFactory`

Ketika perintah `AT+QCFG="ResetFactory"` dieksekusi pada modem Quectel RG501Q-EU:
1. **USB Composition direset**: USB PID kembali ke `0x0800` (mode default tanpa ADB).
2. **Partisi `/opt` ter-wipe**: Server Dropbear SSH dan pustaka pendukung terhapus.
3. **PCIe & Driver Ethernet Nonaktif**: Chipset Realtek RTL8125 tidak termuat, sehingga port fisik LAN (`eth0`) mati total.
4. **Bridge & Routing Terputus**: Port LAN tidak ter-bridge ke `bridge0` dan tidak menyalurkan koneksi internet ke perangkat klien.

Panduan ini memulihkan sistem dari kondisi nol (0) hingga modem kembali normal menjalankan **QManager-GO WebUI** dan port LAN menyalurkan koneksi internet berkecepatan penuh.

---

## 1. Membuka Serial Port & Mengaktifkan ADB (`QADBKEY`)

### A. Bersihkan Kuncian Serial di PC/Router Host
Sebelum berkomunikasi dengan port serial modem (`/dev/ttyUSB*`), hentikan service host yang berpotensi mengunci port:
```bash
sudo systemctl stop ModemManager
sudo killall qmi-proxy 2>/dev/null || true
```

### B. Dapatkan Tantangan Serial Number (SN)
Kirim perintah AT berikut melalui port AT modem (misal `/dev/ttyUSB2` atau `/dev/ttyUSB3`):
```text
AT+QADBKEY?
```
*Contoh Output:*
```text
+QADBKEY: 40079162
OK
```

### C. Hitung Kunci ADB (Python Generator)
Jalankan script Python berikut di PC Host untuk menghitung kunci unlock berdasarkan Serial Number (SN) yang didapat:

```python
#!/usr/bin/env python3
import hashlib
import sys

def generate_qadb_key(sn: str) -> str:
    magic = b"$1$"
    salt = sn.encode("ascii")
    pw = b"SH_adb_quectel"
    
    ctx = hashlib.md5(pw + magic + salt)
    ctx1 = hashlib.md5(pw + salt + pw).digest()
    
    for i in range(len(pw), 0, -16):
        ctx.update(ctx1[:min(i, 16)])
    i = len(pw)
    while i:
        ctx.update(b"\x00" if (i & 1) else pw[:1])
        i >>= 1
    final = ctx.digest()
    
    for i in range(1000):
        c = hashlib.md5()
        c.update(pw if (i & 1) else final)
        if i % 3: c.update(salt)
        if i % 7: c.update(pw)
        c.update(final if (i & 1) else pw)
        final = c.digest()
        
    itoa64 = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
    reorder = [0, 6, 12, 1, 7, 13, 2, 8, 14, 3, 9, 15, 4, 10, 5]
    out = []
    for i in range(0, 15, 3):
        v = final[reorder[i]] | (final[reorder[i+1]] << 8) | (final[reorder[i+2]] << 16)
        for _ in range(4):
            out.append(itoa64[v & 0x3f])
            v >>= 6
    v = final[11]
    out.append(itoa64[v & 0x3f])
    out.append(itoa64[(v >> 6) & 0x3f])
    return ("$1$" + sn + "$" + "".join(out))[12:27]

if __name__ == "__main__":
    sn_input = sys.argv[1] if len(sys.argv) > 1 else "40079162"
    print(f"Generated QADB Key for SN {sn_input}: {generate_qadb_key(sn_input)}")
```

*Contoh Hasil untuk SN `40079162`:* `uw9UiFVcMVpeARv`

### D. Buka Kunci ADB & Atur Komposisi USB
Kirim perintah AT berikut secara berurutan:
```text
AT+QADBKEY="uw9UiFVcMVpeARv"
AT+QCFG="usbcfg",0x2C7C,0x0801,1,1,1,1,1,1,0
AT+CFUN=1,1
```

Setelah modem reboot, verifikasi koneksi ADB dari Host:
```bash
adb devices
# Output yang benar:
# eee3f2ef    device
```

---

## 2. Pemasangan Dropbear SSH & Persistent Storage (`/opt`)

Rootfs bawaan Quectel RG501Q-EU bersifat Read-Only (`/`), sedangkan partisi data yang persisten terhadap reboot berada di `/usrdata`.

### A. Unduh & Ekstrak Dropbear di PC Host
```bash
mkdir -p /tmp/entware_dropbear && cd /tmp/entware_dropbear
curl -sL http://bin.entware.net/armv7sf-k3.2/dropbear_2025.89-1_armv7-3.2.ipk -o dropbear.ipk
tar -xzf dropbear.ipk ./data.tar.gz
tar -xzf ./data.tar.gz
```

### B. Pasang Persistent Bind Mount & Deploy Binary
Jalankan melalui ADB shell:
```bash
# 1. Remount rootfs read-write dan siapkan direktori bind
adb shell "mount -o remount,rw / && mkdir -p /usrdata/opt /opt && mount --bind /usrdata/opt /opt"
adb shell "mkdir -p /opt/bin /opt/sbin /opt/etc/dropbear /opt/lib"

# 2. Push biner Dropbear dari host ke modem
adb push /tmp/entware_dropbear/opt/sbin/dropbear /opt/sbin/dropbear

# 3. Beri izin eksekusi dan buat symlink utilitas Dropbear
adb shell "chmod +x /opt/sbin/dropbear"
adb shell "ln -sf /opt/sbin/dropbear /opt/bin/dropbearkey"
adb shell "ln -sf /opt/sbin/dropbear /opt/bin/dbclient"

# 4. Buat symlink Dynamic Linker & pustaka sistem Linux
adb shell "ln -sf /lib/ld-linux-armhf.so.3 /opt/lib/ld-linux.so.3"
adb shell "ln -sf /usr/lib/libcrypt.so.1 /opt/lib/libcrypt.so.1"
adb shell "ln -sf /lib/libutil.so.1 /opt/lib/libutil.so.1"
adb shell "ln -sf /lib/libc.so.6 /opt/lib/libc.so.6"
adb shell "ln -sf /lib/libgcc_s.so.1 /opt/lib/libgcc_s.so.1"

# 5. Buat Host Key SSH
adb shell "/opt/bin/dropbearkey -t rsa -f /opt/etc/dropbear/dropbear_rsa_host_key"
adb shell "/opt/bin/dropbearkey -t ed25519 -f /opt/etc/dropbear/dropbear_ed25519_host_key"

# 6. Reset Root Password menjadi kosong (Blank root login)
adb shell "sed -i 's/^root:[^:]*:/root::/' /etc/shadow && sed -i 's/^root:[^:]*:/root::/' /usrdata/etc/shadow 2>/dev/null || true"
```

### C. Konfigurasi Systemd Unit Dropbear Persisten
Buat file systemd unit berikut pada modem agar Dropbear otomatis berjalan setiap booting:

1. **Mount Unit (`/lib/systemd/system/opt.mount`):**
```ini
[Unit]
Description=Bind /usrdata/opt to /opt

[Mount]
What=/usrdata/opt
Where=/opt
Type=none
Options=bind

[Install]
WantedBy=multi-user.target
```

2. **Service Unit (`/lib/systemd/system/dropbear.service`):**
```ini
[Unit]
Description=Dropbear SSH Server
After=network.target opt.mount
Requires=opt.mount

[Service]
Type=simple
ExecStart=/opt/sbin/dropbear -F -E -R -B -p 22
Restart=always
RestartSec=2

[Install]
WantedBy=multi-user.target
```

Aktifkan service:
```bash
adb shell "systemctl daemon-reload && systemctl enable opt.mount dropbear.service && systemctl restart dropbear.service"
```

---

## 3. Mengaktifkan Driver Realtek RTL8125 Dual-LAN via PCIe

Pada Quectel RG501Q-EU, port Ethernet ganda ditenagai oleh chip controller Realtek RTL8125 yang terhubung ke bus PCIe Qualcomm SDX55.

### A. Aktifkan PCIe Controller & Driver RTL8125
Kirim via AT command:
```text
AT+QCFG="pcie/mode",1
AT+QETH="eth_driver","r8125",1
AT+CFUN=1,1
```

### B. Verifikasi Antarmuka `eth0`
Setelah modem menyala kembali, jalankan di modem:
```bash
ip link show eth0
```
Jika driver berhasil aktif, antarmuka `eth0` (dan `eth1` jika dual-port aktif) akan terdeteksi.

### C. Persistent Bridge Service (`bridge0` + `eth0`)
Agar traffic port fisik LAN tergabung ke switch/bridge lokal modem, `eth0` harus dimasukkan ke dalam `bridge0`.

Buat unit service `/lib/systemd/system/bridge-eth0.service`:
```ini
[Unit]
Description=Bridge eth0 to bridge0 for LAN ports
After=basic.target

[Service]
Type=oneshot
ExecStart=/bin/sh -c '/sbin/brctl addif bridge0 eth0 || true'
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
```

Aktifkan service:
```bash
adb shell "systemctl daemon-reload && systemctl enable bridge-eth0.service && systemctl start bridge-eth0.service"
```

Verifikasi susunan bridge:
```bash
adb shell "brctl show"
# bridge name     bridge id           STP enabled     interfaces
# bridge0         8000.xxxxxxxxx      no              eth0
#                                                     rndis0
```

---

## 4. Konfigurasi Routing NAT, Dnsmasq, & Internet Gateway LAN

Agar komputer atau router WiFi yang dicolokkan ke port LAN mendapatkan IP secara otomatis (DHCP) dan dapat mengakses internet melalui kartu SIM seluler:

### A. Konfigurasi Dnsmasq untuk LAN (`bridge0`)
Karena rootfs berstatus Read-Only, lease file DHCP harus diarahkan ke direktori ramdisk `/tmp/dnsmasq.leases`.

Buat file `/etc/dnsmasq.d/bridge0.conf`:
```conf
interface=bridge0
dhcp-range=192.168.225.20,192.168.225.200,255.255.255.0,12h
dhcp-option=3,192.168.225.1
dhcp-option=6,1.1.1.1,8.8.8.8
dhcp-leasefile=/tmp/dnsmasq.leases
```

Restart dnsmasq:
```bash
adb shell "systemctl restart dnsmasq"
```

### B. Aktifkan IPv4 Forwarding & Aturan NAT Masquerade
Pastikan kernel mengizinkan paket forwarding antar-interface:
```bash
# Aktifkan IP forwarding
adb shell "echo 1 > /proc/sys/net/ipv4/ip_forward"

# Aturan iptables NAT Masquerade menuju antarmuka seluler (rmnet_data0 / rmnet_data+)
adb shell "iptables -t nat -A POSTROUTING -o rmnet_data+ -j MASQUERADE"
adb shell "iptables -A FORWARD -i bridge0 -o rmnet_data+ -j ACCEPT"
adb shell "iptables -A FORWARD -i rmnet_data+ -o bridge0 -m state --state RELATED,ESTABLISHED -j ACCEPT"
```

### C. Pastikan Backhaul Data Seluler Aktif
Modem menggunakan Qualcomm Connection Manager (`QCMAP`) untuk mengatur dial data seluler:
```bash
adb shell "systemctl enable setup-bridge0 QCMAP_ConnectionManagerd"
adb shell "systemctl start QCMAP_ConnectionManagerd"
```

---

## 5. Pemasangan & Menjalankan QManager-GO Single Binary

Setelah SSH, ADB, driver RTL8125, dan routing LAN aktif, pasang **QManager-GO**:

### A. Deploy Binary Bawaan (`qmanager-core-armv7`)
Dari root direktori repositori `QManager-GO` di komputer host:
```bash
# Melalui script otomatis 1-klik:
./deploy.sh adb

# Atau manual via ADB:
adb push backend/dist/qmanager-core-armv7 /usrdata/qmanager-core
adb shell "chmod +x /usrdata/qmanager-core"
```

### B. Konfigurasi Systemd Service Unit (`/lib/systemd/system/qmanager-core.service`)
```ini
[Unit]
Description=QManager Go Core Service
After=basic.target

[Service]
Type=simple
ExecStart=/usrdata/qmanager-core
Restart=always
RestartSec=3
KillMode=process
Environment=PORT=80
Environment=AT_DEVICE=/dev/smd11

[Install]
WantedBy=multi-user.target
```

### C. Jalankan Service & Verifikasi
```bash
adb shell "systemctl daemon-reload && systemctl enable qmanager-core && systemctl restart qmanager-core"
```

Akses antarmuka web dashboard QManager-GO melalui peramban:
- **URL Dashboard:** `http://192.168.225.1`
- **SSH Login:** `ssh root@192.168.225.1` (tanpa password)
- **Status LAN:** Port fisik LAN kini membagikan IP DHCP dan langsung terhubung ke internet seluler.

---

## 🔗 Referensi Terkait
- [Hardware Support Matrix](../HARDWARE-SUPPORT.md)
- [QManager Go Deployment Guide](../DEPLOYMENT.md)
- [LAN Configuration & DHCP Architecture](../features/lan-config.md)
