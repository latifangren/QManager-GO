// =============================================================================
// simple-mode-utils.ts — Shared helpers for Tower Locking "Simple Mode"
// =============================================================================
// Derives selectable carrier options from the live ModemStatus QCAINFO data
// (network.carrier_components) and provides composite-value helpers for
// <Select> dropdowns that need to round-trip an (earfcn, pci) pair.
// =============================================================================

import type { ModemStatus, CarrierComponent } from "@/types/modem-status";

/** A carrier option suitable for rendering in a Simple Mode dropdown */
export interface CarrierOption {
  /** Channel number (EARFCN for LTE, ARFCN for NR) */
  earfcn: number;
  /** Physical Cell ID */
  pci: number;
  /** 3GPP band string from QCAINFO, e.g. "B3" or "N41" */
  band: string;
  /** Numeric band, e.g. 3 or 41 — null if it cannot be parsed */
  bandNumber: number | null;
  /** PCC or SCC role */
  type: "PCC" | "SCC";
  /** Bandwidth in MHz (0 if unknown) */
  bandwidthMhz: number;
  /** RSRP (dBm) — may be null */
  rsrp: number | null;
  /** RSRQ (dB) — may be null */
  rsrq: number | null;
  /** SINR (dB) — may be null */
  sinr: number | null;
}

/**
 * Extract numeric band from a 3GPP band string ("B3" → 3, "N41" → 41).
 */
export function extractBandNumber(band: string | null | undefined): number | null {
  if (!band) return null;
  const match = band.match(/\d+/);
  return match ? parseInt(match[0], 10) : null;
}

function toOption(c: CarrierComponent): CarrierOption | null {
  if (c.earfcn == null || c.pci == null) return null;
  return {
    earfcn: c.earfcn,
    pci: c.pci,
    band: c.band,
    bandNumber: extractBandNumber(c.band),
    type: c.type,
    bandwidthMhz: c.bandwidth_mhz,
    rsrp: c.rsrp,
    rsrq: c.rsrq,
    sinr: c.sinr,
  };
}

function dedupAndSort(options: CarrierOption[]): CarrierOption[] {
  // Dedup on (earfcn, pci); PCC ranks first, then SCC, then by RSRP descending.
  const seen = new Set<string>();
  const out: CarrierOption[] = [];
  const sorted = [...options].sort((a, b) => {
    if (a.type !== b.type) return a.type === "PCC" ? -1 : 1;
    const ar = a.rsrp ?? -200;
    const br = b.rsrp ?? -200;
    return br - ar;
  });
  for (const o of sorted) {
    const key = `${o.earfcn}-${o.pci}`;
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(o);
  }
  return out;
}

/** Extract LTE carriers (PCC + LTE SCCs) suitable for the LTE Simple Mode dropdown. */
export function lteCarriersFromQcainfo(modemData: ModemStatus): CarrierOption[] {
  const components = modemData?.network?.carrier_components ?? [];
  const opts: CarrierOption[] = [];
  for (const c of components) {
    if (c.technology !== "LTE") continue;
    const o = toOption(c);
    if (o) opts.push(o);
  }
  return dedupAndSort(opts);
}

/** Extract NR carriers (PCC + NR SCCs) suitable for the NR-SA Simple Mode dropdown. */
export function nrCarriersFromQcainfo(modemData: ModemStatus): CarrierOption[] {
  const components = modemData?.network?.carrier_components ?? [];
  const opts: CarrierOption[] = [];
  for (const c of components) {
    if (c.technology !== "NR") continue;
    const o = toOption(c);
    if (o) opts.push(o);
  }
  return dedupAndSort(opts);
}

/**
 * Reasonable SCS default for an NR band when the live serving cell isn't the
 * one being locked. FR1 mid-band (n7/n38/n41/n77/n78/n79) → 30 kHz, low-band
 * (sub-1 GHz) → 15 kHz, FR2 (>n257) → 120 kHz.
 */
export function defaultScsForBand(bandNumber: number | null): number | null {
  if (bandNumber == null) return null;
  if (bandNumber >= 257) return 120; // FR2
  // Low-band NR (sub-1 GHz typical): n5, n8, n12, n13, n14, n18, n20, n26, n28, n71
  const lowBand = new Set([5, 8, 12, 13, 14, 18, 20, 26, 28, 71]);
  if (lowBand.has(bandNumber)) return 15;
  return 30;
}

/** Encode (earfcn, pci) into a stable composite string for <Select value="..."> */
export function compositeValue(earfcn: number, pci: number): string {
  return `${earfcn}-${pci}`;
}

/** Decode a composite string back to its (earfcn, pci) parts. */
export function parseCompositeValue(
  value: string,
): { earfcn: number; pci: number } | null {
  const [a, b] = value.split("-");
  const earfcn = parseInt(a ?? "", 10);
  const pci = parseInt(b ?? "", 10);
  if (Number.isNaN(earfcn) || Number.isNaN(pci)) return null;
  return { earfcn, pci };
}
