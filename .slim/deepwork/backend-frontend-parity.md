# Deepwork Progress: Backend-Frontend Parity Remediation

## Task Overview
Remediate all API, routing, serialization, and schema mismatches between the Go backend daemon (`backend/`) and Next.js 15 frontend (`frontend/`) in the QManager project.

---

## Phased Implementation Plan

- **Phase 1: Routing, Link Speed & Static Asset Delivery**
  - Fix Ethernet POST link speed handler and router binding.
  - Mount dynamic `/locales-packs/` HTTP static file server in router.
  - Implement CDN hostlist default template restore (`action: "restore_hostlist"`).
  - Handle bodyless `POST` query fallback in `update.go` (`action=reboot_ack`).
  - **Ownership:** `@fixer` (implementation), `@fast-generic` (build/unit tests)
  - **Oracle Gate 1:** Verify routing, link speed, and asset delivery contracts.

- **Phase 2: Cellular Control & Protocol SerDe (Band Locking, Tower Locking & APN/WAN)**
  - Support string array decoding for `lte_bands` and `nr_bands` in `cellular.go:SetBandsRequest` (dual format: array & string).
  - Format `current` bands as string arrays and populate `supported` bands object in `GetBands`.
  - Fix `cellular_apn.go:ApnProfile` boolean fields (`has_password`, `enabled`, `is_active`) and add root metadata (`max_profiles`, `data_source`).
  - Expose `/api/v1/cellular/profiles/deactivate` for REST parity with CGI.
  - **Ownership:** `@fixer` (implementation), `@fast-generic` (build/unit tests)
  - **Oracle Gate 2:** Review AT command safety, debounce, and cellular SerDe correctness.

- **Phase 3: System, Auth & Monitoring Parity (Config SerDe, Alerts, Auth Check)**
  - Refactor `system.go:GetConfig` to return top-level `settings` and `scheduled_reboot` with `days: []int`.
  - Refactor `system.go:SaveConfig` with action-based partial updates (`save_settings`, `save_scheduled_reboot`) to prevent destructive configuration overwrites.
  - Implement action branching in `alerts_handler.go` (`get_log`, `clear_log`, `test`).
  - Add `session_expires_at` to `auth.go:Check` and structured `lockout` to `auth.go:Login`.
  - **Ownership:** `@fixer` (implementation), `@fast-generic` (build/unit tests)
  - **Oracle Gate 3:** Review persistence safety (NAND zero-wear atomic write compliance) and auth security.

- **Phase 4: Comprehensive Verification, Regression Testing & Parity Validation**
  - Run complete backend test suite (`go test -v ./...`).
  - Add regression unit tests for all remediated endpoints.
  - Run ARMv7 cross-compilation validation (`GOOS=linux GOARCH=arm GOARM=7 go build`).
  - **Ownership:** `@fast-generic` (test runner & build validation), `@oracle` (final comprehensive review)
  - **Oracle Gate 4:** Final architecture, parity, and compliance sign-off.

---

## Current Status
- **Phase 1: Routing, Link Speed & Static Asset Delivery** — COMPLETED & VERIFIED (Gate 1 Passed).
- **Phase 2: Cellular Control & Protocol SerDe** — COMPLETED & VERIFIED (Gate 2 Passed).
- **Phase 3: System, Auth & Monitoring Parity** — COMPLETED & VERIFIED (Gate 3 Passed).
- **Phase 4: Comprehensive Verification & ARMv7 Build** — COMPLETED & VERIFIED (Gate 4 Final Sign-off Passed).
  - All 7 packages passed uncached unit & router tests (`go test ./...`).
  - Standalone ARMv7 cross-compilation verified clean (`GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0`).
  - NAND zero-flash wear atomic writes and 1970 clock-step guards verified.
  - Zero git working tree debris. Status: READY.
