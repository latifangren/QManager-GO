# Audit & Komparasi Arsitektur QManager

Dokumentasi hasil audit komparatif mendalam antara codebase **Legacy (`QManager-GO` / `QManager-GO-archived`)** dan codebase **Base Baru (`QManager-`)**.
"F:\GITHUB\QManager-GO-archived"
"F:\GITHUB\.tmp\QManager-GO"
---

## 1. Ringkasan Eksekutif

| Parameter | Legacy / Archived (`QManager-GO` / `QManager-GO-archived`) | Base Baru (`QManager-`) | Status Evaluasi |
| :--- | :--- | :--- | :--- |
| **Arsitektur Appliance** | Monorepo hybrid datar, multi-binary / Go server + disk fallback frontend | **Single Standalone Binary** (`qmanager-armv7`) embedding static Next.js 15 (`//go:embed dist/*`) | Sesuai Standar |
| **NAND Flash Wear Policy** | ⚠️ Melanggar (Sesi login `/etc/qmanager/sessions.json` & status/telemetry NDJSON ditulis berkala ke flash/disk) | 🛡️ **100% RAM-First** (In-memory ring buffers, zero telemetry disk writes) | Lolos |
| **AT Command Execution** | ⚠️ Single Mutex FIFO + Subprocess spawn (`qcmd` / `/usr/bin/atcli_smd11`) | ⚡ **Actor Model + 3-Tier Priority Queue** (`/dev/smd11` direct IO via POSIX lock) | Lolos |
| **Recovery Watchdog** | ⚠️ 2-Tier Watchdog sederhana (TCP dial 53/80 -> CFUN -> Hard Reboot) | 🛡️ **4-Tier Watchdog** + Proteksi 1970 Clock-Step Boot Window | Lolos |
| **Router & API** | `http.ServeMux` standar + 800+ baris file handler monolithic | **Chi Router v5** (REST `/api/v1/*` + Backward-compatible CGI shim `/cgi-bin/quecmanager/*` terisolasi per domain) | Lolos |
| **Target Hardware** | Quectel Snapdragon X55 (Cortex-A7) | Quectel RG501Q-EU (SDX55) & RM520N-GL (SDX65) | Sesuai Target |

---

## 2. Perbandingan Arsitektur & Concurrency

```
[Legacy / Archived Model: Mutex Lock & Subprocess Spawn]
UI Request ------\
Watchdog (CFUN) --+--> [ sync.Mutex ] --> fork/exec "qcmd" / "atcli_smd11" --> /dev/smd11 (CPU churn, Blockage)
10s/1s Poller ---/

[Model Baru: 3-Tier Actor Engine]
Priority High   (Watchdog/CFUN)  ----[ Ch High   (cap 4)  ]---\
Priority Normal (UI Actions/APN) ----[ Ch Normal (cap 16) ]----> [ Actor Worker Loop ] --> Direct POSIX Lock -> /dev/smd11
Priority Low    (1Hz Signal Poll) ---[ Ch Low    (cap 1)  ]---/  (Drop/Yield if High/Normal Busy)
```

### Analisis Concurrency:
1. **Pencegahan Starvation**: Pada codebase baru, antrean `PriorityLow` (1 Hz signal poller `+QENG`, `+QCAINFO`) otomatis di-drop atau yield jika antrean `PriorityHigh` (Watchdog radio recovery, emergency CFUN) atau `PriorityNormal` (konfigurasi user dari web UI) sedang terisi.
2. **Eliminasi Subprocess Churn**: Legacy mengeksekusi binary `qcmd` melalui `exec.Command`, menyebabkan fork/exec overhead tinggi pada single-core Cortex-A7. Codebase baru menggunakan direct character device IO (`/dev/smd11`) dengan POSIX advisory file locking (`flock`).
3. **Goroutine Lifecycle & Context**: Codebase baru dilengkapi bounded context timeouts (5s default, 30s network/scan commands) dan clean broadcast cancellation channel (`stopChan`).

---

## 3. Flash Wear & Storage Handling (Zero Flash Wear Compliance)

NAND flash (UBIFS) pada modem Quectel memiliki batas write cycles yang terbatas. Menulis file setiap beberapa detik akan merusak flash memori (bricking device).

| Komponen | Legacy / Archived (`QManager-GO-archived`) | Base Baru (`QManager-`) | Dampak Keandalan |
| :--- | :--- | :--- | :--- |
| **Session Persistence** | Ditulis ke flash `/etc/qmanager/sessions.json` via `os.WriteFile` setiap login. | **100% In-Memory RAM Map** dengan 24h TTL expiration. | Mencegah keausan NAND saat login berulang. |
| **Telemetry History** | Menulis ulang file status `/tmp/qmanager_status.json` dan NDJSON `/tmp/qmanager_*_history.ndjson`. | **In-Memory Fixed Circular Ring Buffer** (1800 sinyal, 1440 ping, 500 events). | Zero flash wear, alokasi memori terkendali. |
| **Daemon Logging** | Menulis log terus menerus ke file disk `/tmp/qmanager.log`. | **In-Memory Ring Buffer (500–1000 baris)** disajikan via `/api/v1/system/logs`. | Log tersimpan aman di RAM tanpa overhead I/O disk. |
| **Mutasi Konfigurasi** | Parsing file UCI OpenWrt (`pkg/uci`) dan teks tersebar di `/etc/qmanager/*.conf` tanpa atomic guarantee. | **Atomic Temp-and-Rename Pattern**: Validasi `os.Lstat` (tolak symlink/dir), tulis ke `.tmp.<nano>`, eksekusi `f.Sync()`, lalu atomic `os.Rename`. | Mencegah korupsi konfigurasi saat power loss. |

---

## 4. Matriks Fitur Backend & Endpoint Mapping

| Fitur / Domain | Legacy / Archived | Base Baru (`QManager-`) | REST API Endpoint | CGI Shim Path |
| :--- | :---: | :---: | :--- | :--- |
| **Authentication** | ✅ (Disk sessions) | ✅ (RAM sessions) | `POST /api/v1/auth/login` | `/cgi-bin/quecmanager/auth/login.sh` |
| **Live Telemetry & Signals** | ✅ (10s poll) | ✅ (1Hz zero-alloc) | `GET /api/v1/cellular/status` | `/cgi-bin/quecmanager/at_cmd/fetch_data.sh` |
| **Signal History** | ⚠️ NDJSON file | ✅ In-Memory Ring Buffer | `GET /api/v1/cellular/signal-history` | `/cgi-bin/quecmanager/at_cmd/fetch_signal_history.sh` |
| **Band Locking & Failover** | ✅ | ✅ | `POST /api/v1/bands/lock` | `/cgi-bin/quecmanager/bands/lock.sh` |
| **Tower Locking & Schedule** | ✅ | ✅ | `POST /api/v1/tower-locking/lock` | `/cgi-bin/quecmanager/at_cmd/tower_lock.sh` |
| **Frequency Locking & Calc** | ✅ | ✅ | `POST /api/v1/frequency-locking/lock`| `/cgi-bin/quecmanager/at_cmd/freq_lock.sh` |
| **Cell & Neighbour Scanner** | ✅ | ✅ | `POST /api/v1/cell-scanner/start` | `/cgi-bin/quecmanager/at_cmd/cell_scan_start.sh` |
| **SIM Profiles & Scenarios** | ✅ | ✅ | `GET /api/v1/sim-profiles` | `/cgi-bin/quecmanager/cellular/sim_profile.sh` |
| **SIM Registry (Known SIMs)**| ✅ | ✅ | `GET /api/v1/system/sim-registry` | `/cgi-bin/quecmanager/cellular/sim_registry.sh` |
| **APN Management** | ✅ | ✅ | `POST /api/v1/apn/set` | `/cgi-bin/quecmanager/cellular/apn.sh` |
| **IMEI Repair/Custom** | ✅ | ✅ | `POST /api/v1/imei/set` | `/cgi-bin/quecmanager/cellular/imei.sh` |
| **MBN Profile Engine** | ✅ | ✅ | `POST /api/v1/mbn/select` | `/cgi-bin/quecmanager/cellular/mbn.sh` |
| **FPLMN Clear & Read** | ✅ | ✅ | `POST /api/v1/fplmn/clear` | `/cgi-bin/quecmanager/cellular/fplmn.sh` |
| **IP Passthrough (IPPT)** | ✅ | ✅ | `POST /api/v1/network/ippt` | `/cgi-bin/quecmanager/network/ippt.sh` |
| **MTU & DNS Manager** | ✅ | ✅ | `POST /api/v1/network/mtu` | `/cgi-bin/quecmanager/network/mtu.sh` |
| **Traffic Engine (DPI/Video)**| ⚠️ Zapret custom | ✅ Clean DPI & dnsmasq | `POST /api/v1/network/dpi` | `/cgi-bin/quecmanager/network/dpi.sh` |
| **VPN Integration** | ✅ (Netbird + Tailscale) | ✅ (Tailscale daemon) | `GET /api/v1/network/tailscale` | `/cgi-bin/quecmanager/network/tailscale.sh` |
| **4-Tier Watchdog (Watchcat)**| ⚠️ 2-Tier sederhana | ✅ **4-Tier + 1970 Guard** | `POST /api/v1/watchdog/config` | `/cgi-bin/quecmanager/system/watchdog.sh` |
| **SMS Center & Forwarder** | ⚠️ Basic SMS | ✅ **SMS + Telegram/Webhook** | `POST /api/v1/sms/send` | `/cgi-bin/quecmanager/cellular/sms.sh` |
| **Ookla / Native Speedtest** | ✅ | ✅ | `POST /api/v1/diagnostics/speedtest`| `/cgi-bin/quecmanager/system/speedtest.sh` |
| **System Logs** | ⚠️ Disk read | ✅ RAM Ring Buffer | `GET /api/v1/system/logs` | `/cgi-bin/quecmanager/system/logs.sh` |
| **Health Check & Diagnostics**| ⚠️ Parsial | ✅ Tarball Export | `GET /api/v1/system/diagnostics` | `/cgi-bin/quecmanager/system/diagnostics.sh` |
| **Dynamic Language Packs** | ⚠️ Static JSON | ✅ Runtime Packs | `GET /api/v1/system/i18n` | `/cgi-bin/quecmanager/system/i18n.sh` |

---

## 5. Analisis Frontend & Integrasi

| Aspek | `QManager-GO-archived` | `QManager-` |
|---|---|---|
| **Lokasi Codebase** | Tersebar di root folder (`/src/app`, `/src/components`, `/src/lib`, `/src/types`). | Terisolasi rapi di folder `/frontend/`. |
| **Komponen & Desain UI** | Antarmuka dashboard standar Radix UI / Shadcn, SSE stream listener sederhana, masih banyak rely pada CGI `.sh` mock. | UI Modern Next.js 15 App Router + React 19, Tailwind CSS v4, custom status tiles, live metric bar, animated HUD indicators, TickingValue components, dan full i18n dynamic switching. |
| **Sistem Routing API** | Frontend memanggil URL legacy `/cgi-bin/quecmanager/*/*.sh`. | Mendukung backward compatibility route CGI lama sekaligus REST API terstruktur modern (`/api/v1/*`). |

---

## 6. Daftar Bug, Kelemahan & Peluang Perbaikan di Base Baru

1. **Auth Middleware Enforcement di Protected Routes**:
   - Lokasi: `backend/internal/api/router/router.go`
   - Isu: Sub-router protected routes saat ini masih menggunakan stub pass-through tanpa memvalidasi bearer token.
   - Solusi: Aktifkan middleware validasi session token in-memory sebelum dispatch ke domain handler.
2. **Speedtest File I/O di `/tmp`**:
   - Lokasi: `backend/internal/api/handlers/speedtest.go`
   - Isu: Progress speedtest masih ditulis ke file temporer `/tmp/qmanager_speedtest_progress.json`.
   - Solusi: Simpan progress dan state runner speedtest di in-memory struct dengan `sync.RWMutex`.
3. **SMS PDU / Multipart Handling**:
   - Lokasi: `backend/internal/api/handlers/sms.go`
   - Isu: Masih menggunakan AT text mode (`AT+CMGF=1`). Belum menangani rekombinasi SMS panjang (multipart / concatenated) atau karakter UCS2/Unicode non-ASCII.
   - Solusi: Sediakan modul parser PDU di `internal/atengine/pdu.go`.
4. **Pemeriksaan Lock File saat Device Restart**:
   - Lokasi: `backend/internal/atengine/transport_lock_posix.go`
   - Isu: Jika proses mati paksa, lock file `/var/lock/qmanager_smd11.lock` perlu dipastikan bersih saat startup.
   - Solusi: Handler startup memeriksa kepemilikan PID sebelum acquire lock.

---

## 7. Kesimpulan & Rekomendasi Langkah Kerja

1. **Fondasi Arsitektur Base Baru Sangat Solid**: Pemisahan domain di `backend/internal/` sudah mematuhi spesifikasi embedded Linux Cortex-A7 dan Zero Flash Wear Policy.
2. **Fokus Prioritas Selanjutnya**:
   - Pasang proteksi Bearer Token di router Chi.
   - Bersihkan sisa-sisa penulisan disk di handler speedtest & scanner.
   - Lakukan uji build cross-compile `make build` dan uji kompatibilitas serial device `/dev/smd11` di perangkat target.
