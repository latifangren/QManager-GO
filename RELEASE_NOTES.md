# QManager-GO Release Notes

---

## 🚀 QManager-GO v1.2.0 (Stable Release)

Selamat datang di rilis stabil resmi pertama **QManager-GO v1.2.0**! 🎉

Rilis ini merupakan tonggak besar transformasi QManager-GO dari fase beta menjadi solusi manajemen modem yang sepenuhnya matang, tangguh (*resilient*), mandiri (*zero-dependency*), dan berorientasi *zero-touch*. Tidak ada lagi kebutuhan untuk mengatur bridge LAN, DHCP server, atau menginstal Dropbear/OpenSSH secara manual setelah factory reset modem.

---

### 🌟 Major Highlights & Inovasi Utama (v1.2.0 Stable)

#### 🔒 1. Native Standalone Pure Go SSH Server (Port :22)
* **Zero Dropbear / OpenSSH Dependency:** Server SSH standalone yang diimplementasikan 100% menggunakan native Go (`golang.org/x/crypto/ssh`).
* **Dynamic Linux Shadow Authentication:** Mengautentikasi user `root` langsung terhadap hash `/etc/shadow` (MD5 crypt `$1$` dan standard crypt) secara in-process tanpa CGO.
* **Authorized Public Keys Management:** Mendukung login tanpa password berbasis public key (`/etc/qmanager/ssh/authorized_keys`) yang dapat ditambahkan/dihapus langsung via WebUI.
* **Auto Host Key Generation:** Otomatis men-generate host key Ed25519 (`/etc/qmanager/ssh/ssh_host_ed25519_key`) pada boot pertama jika belum tersedia.
* **Port Conflict Detection & Auto-Purge:** Installer dan runtime mendeteksi serta menonaktifkan instance Dropbear lama secara otomatis agar tidak terjadi bentrok port 22.

#### 🌐 2. Zero-Touch Auto TLS & Dual HTTP/HTTPS Listener
* **Dual Port Listener:** Mendukung akses WebUI simultan pada HTTP (Port `80`) dan HTTPS terenkripsi (Port `443`).
* **On-the-Fly Self-Signed Certificates:** Engine internal pure Go ECDSA P-256 (`internal/tlsgen`) secara otomatis men-generate sertifikat TLS yang valid saat boot pertama tanpa memerlukan tool eksternal `openssl`.

#### 🌐 3. Zero-Touch Idempotent LAN & Gateway Provisioning
* **Automatic PCIe Ethernet & Bridge Binding:** Otomatis mendeteksi ethernet adapter board M.2 (Realtek `r8125` / `eth0`), membuat interface `bridge0` (`192.168.225.1`), dan mem-binding `eth0` ke bridge.
* **Rogue Link-Local Address Auto-Flush:** Otomatis membersihkan IP rogue APIPA (`169.254.x.x`) pada interface ethernet agar alokasi IP LAN klien selalu bersih.
* **Automated DHCP Subnet Generation:** Otomatis menghasilkan `/etc/dnsmasq.conf` yang sinkron dengan subnet bridge dan mengelola reload `dnsmasq` secara berkala.
* **Cellular WWAN Backhaul Auto-Fix:** Memperbaiki konfigurasi `mobileap_cfg.xml` Qualcomm agar trafik internet seluler (`rmnet_data0`) langsung ter-forward mulus ke klien LAN ethernet.

#### ⚡ 4. Native Pure Go Speedtest Engine
* **Eliminated External Ookla CLI:** Mengganti dependensi binary eksternal `speedtest` dengan pure Go speedtest engine (`github.com/showwin/speedtest-go`).
* **Real-time Metrics:** Pengujian download, upload, ping, dan jitter langsung dari memory modem dengan pelaporan status streaming ke WebUI.

#### 📨 5. Native Pure Go SMS & PDU Engine (Zero `sms_tool`)
* **3GPP PDU Decoder & Multipart Reassembly:** Decoder PDU murni dalam Go yang mendukung 7-bit GSM default alphabet, 8-bit data, dan UCS2 / UTF-16 decoding.
* **UDH Multipart Concatenation:** Otomatis menggabungkan SMS panjang terfragmentasi (IE Identifier `0x00` & `0x08`) secara in-memory.
* **Direct Serial AT Invocation:** Pembacaan dan pengiriman SMS (`AT+CMGL`, `AT+CMGS`, `AT+CMGD`, `AT+CPMS`) langsung dieksekusi via in-process AT Engine (`/dev/smd11`).
* **Binary Embed Cleanup:** Menghapus binary C eksternal `sms_tool` (~430 KB) dari repositori, menghemat konsumsi NAND flash modem.

#### ⚡ 6. In-Process ICMP Latency Engine & Kernel MTU Controller
* **Raw Socket Latency Probe:** Mengganti pemanggilan subprocess `/bin/ping` (yang sebelumnya berjalan ribuan kali per jam) dengan raw ICMP Echo socket (`SOCK_RAW`/`IPPROTO_ICMP`) in-process dengan fallback TCP dialer.
* **Direct MTU Kernel Syscall:** Pengubahan MTU pada antarmuka seluler dieksekusi langsung lewat kernel syscall `ioctl(SIOCSIFMTU)` tanpa fork CLI `ip link`.
* **Direct Sysfs Ethernet Reader:** Membaca status link LAN (`eth0`), negosiasi speed, duplex, MTU, dan traffic counter langsung via sysfs kernel interface.

#### 🔄 7. Native Kernel Reboot Syscall & Direct Process Signaling
* **Kernel Reboot Syscall:** Mengganti shell out `reboot` dengan atomic Linux kernel syscall `syscall.Reboot(LINUX_REBOOT_CMD_RESTART)`.
* **Direct POSIX Signals:** Mengganti `killall -HUP dnsmasq` dan `pkill tpws` dengan direct PID signal dispatch (`syscall.SIGHUP` / `syscall.SIGKILL`) via pembacaan `/proc` dan PID file.
* **Kernel Log Reader:** Membaca log ring buffer kernel langsung dari `/dev/kmsg` tanpa fork subprocess `dmesg`.

---

### 📋 Changelog Detail (v1.1.0-beta -> v1.2.0)
* `12cba3aa` - **feat(sys):** implement native kernel reboot syscall, direct process signaling and /dev/kmsg log reader
* `6f18f6c2` - **feat(net):** implement native in-process ICMP probe and kernel ioctl MTU controller
* `2e79d8d0` - **fix(sms):** refine CPMS storage parser and ensure test suite passes 100%
* `e85e898e` - **feat(sms):** replace external sms_tool with native pure Go PDU engine and multipart UDH reassembly
* `0c8e26cf` - **feat(speedtest):** replace external ookla cli with native pure Go speedtest engine
* `d43ae078` - **docs:** update README, deploy guides, and release notes for Native SSH, Auto-TLS, and Zero-Touch LAN provisioning
* `faef44b2` - **feat(network):** add zero-touch idempotent LAN provisioning and self-healing bridge/dhcp daemon
* `e5d5414a` - **feat(ssh):** add port conflict detection, UI banner alert, and dropbear auto-purge in installer
* `8eeee71d` - **feat(ssh):** add full SSH server management UI and backend API (toggle, custom port, authorized keys, password)
* `e236386f` - **feat(sshd):** implement standalone native Go SSH server with shadow auth and auto hostkey
* `b6d863a0` - **feat(tls):** auto-generate self-signed ECDSA certificates for dual HTTP/HTTPS support

---

## 📦 QManager-GO v1.1.0-beta

Rilis **v1.1.0-beta** fokus pada optimasi performa awal, pengenalan direct POSIX syscall AT transport, dan streaming telemetri vnStat.

### 🌟 Highlights (v1.1.0-beta)
* **Direct POSIX Syscall AT Transport:** Mengganti wrapper shell (`/usr/bin/qcmd`) dengan native Go AT engine melalui POSIX raw syscalls (`syscall.Open`, `syscall.Write`, `syscall.Select`) di `/dev/smd11`.
* **Reduksi Beban CPU:** Menurunkan fork process kernel dari **67 forks/detik menjadi 1 fork/detik** (CPU idle meningkat hingga 80%–85%).
* **Real-Time vnStat Bandwidth Monitoring & SSE Stream:** Streaming data bandwidth real-time via Server-Sent Events (`/api/v1/telemetry/stream`).
* **Dynamic Adaptive Polling:** Penyesuaian interval polling telemetri backend secara dinamis berdasarkan status fokus WebUI (Active, Balanced, Low Power).
* **Security Hardening:** Enforced token authentication pada WebSocket PTY console (`/console/ws`).

### 📋 Changelog (v1.0.0-beta -> v1.1.0-beta)
* `42ee6250` - **feat(sms):** embed `sms_tool` with auto-restoration and update transport docs
* `9352ae7c` - **feat(engine):** implement posix raw syscall at transport engine
* `11eb58a9` - **fix(transport):** clean non-standard output from Rust atcli binary
* `119642b3` - **fix(atengine):** sanitize raw serial responses and strip echo artifacts
* `7ec2f447` - **feat(telemetry):** add adaptive dynamic polling cadence sync
* `0a80e18d` - **feat(telemetry):** add vnstat realtime bandwidth stream and SSE endpoint
* `8d5be144` - **fix(auth):** enforce strict auth checks on web console websocket
* `7d5ab797` - **feat(system):** add automated hardware recovery and factory reset scripts
