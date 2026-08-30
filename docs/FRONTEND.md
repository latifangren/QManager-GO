# QManager Frontend Guide

This document details the frontend architecture, component structure, custom hooks, and build pipeline for the Next.js 15 web UI embedded within the QManager standalone Go binary.

---

## Tech Stack

| Technology | Version / Purpose |
|---|---|
| **Framework** | Next.js 15 (App Router, Static Export via `output: "export"`) |
| **UI Library** | React 19 |
| **Language** | TypeScript 5.x |
| **Styling** | Tailwind CSS v4, OKLCH dynamic color system |
| **Primitives** | shadcn/ui (Radix UI component engine) |
| **Charts** | Recharts (Signal history, ping latency trends) |
| **State & Forms** | React Hook Form + Zod validation |
| **Icons** | Lucide React |
| **Package Manager** | Bun (or Node.js 20+) |

---

## Directory Structure

All frontend source code resides under the `frontend/` directory:

```text
frontend/
├── app/                            # Next.js App Router Pages
│   ├── layout.tsx                  # Root layout & providers
│   ├── page.tsx                    # Route redirect → /dashboard
│   ├── globals.css                 # OKLCH color system & base styling
│   ├── dashboard/                  # Overview signal & performance dashboard
│   ├── cellular/                   # Cellular management & diagnostic hubs
│   │   ├── settings/               # APN, IMEI, network priority, FPLMN
│   │   ├── cell-locking/           # Band, frequency, and tower locking
│   │   ├── cell-scanner/           # Cell scan & frequency calculator
│   │   ├── custom-profiles/        # ICCID-bound SIM profile engine
│   │   └── sms/                    # SMS Inbox & composition
│   ├── local-network/              # Ethernet, MTU, TTL/HL, IP Passthrough, DNS
│   ├── monitoring/                 # Latency charts, watchdog, alerts, logs
│   ├── system-settings/            # Preferences, scheduled operations, OTA
│   └── about-device/               # Hardware profile, SoC, firmware version
├── components/                     # Modular React Components
│   ├── ui/                         # Base shadcn primitives (button, card, dialog, etc.)
│   ├── cellular/                   # Cellular cards, tiles, and tables
│   ├── dashboard/                  # Real-time dashboard widgets
│   ├── local-network/              # Network configuration widgets
│   └── monitoring/                 # Telemetry & alert management components
├── hooks/                          # Custom React Hooks for API polling & mutation
├── types/                          # TypeScript definitions & API schemas
├── lib/                            # Helpers (authFetch, formatters, CSV export)
├── constants/                      # Static metadata (MNO presets, band definitions)
└── public/                         # Brand assets, SVGs, icons
```

---

## Build & Embedding Pipeline

When compiling the full appliance, the frontend is statically exported and embedded into the Go binary:

1. **Static Build**:
   ```bash
   cd frontend && bun run build
   ```
   Generates optimized HTML/CSS/JS artifacts in `frontend/out/`.

2. **Sync to Backend**:
   `Makefile` automatically copies `frontend/out/*` to `backend/cmd/qmanager/dist/`.

3. **Go Embedding**:
   `backend/cmd/qmanager/main.go` embeds `dist/` via `//go:embed dist/*` and serves it via the Chi router's single-page application (SPA) handler.
