"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { authFetch } from "@/lib/auth-fetch";

// =============================================================================
// Bandwidth & vnStat Data Types
// =============================================================================

export interface HourlyBucket {
  hour: string;
  timestamp: number;
  rx_bytes: number;
  tx_bytes: number;
}

export interface DailyBucket {
  date: string;
  timestamp: number;
  rx_bytes: number;
  tx_bytes: number;
}

export interface MonthlyBucket {
  month: string;
  timestamp: number;
  rx_bytes: number;
  tx_bytes: number;
}

export interface RealtimePoint {
  timestamp: number;
  rx_bps: number;
  tx_bps: number;
}

export interface IfaceSnapshot {
  name: string;
  current_rx_bps: number;
  current_tx_bps: number;
  total_rx_bytes: number;
  total_tx_bytes: number;
  today_rx_bytes: number;
  today_tx_bytes: number;
  realtime: RealtimePoint[];
  hourly: HourlyBucket[];
  daily: DailyBucket[];
  monthly: MonthlyBucket[];
}

export interface BandwidthSnapshot {
  timestamp: number;
  interfaces: Record<string, IfaceSnapshot>;
  default_interface: string;
}

export interface UseBandwidthReturn {
  data: BandwidthSnapshot | null;
  currentIface: IfaceSnapshot | null;
  interfaceNames: string[];
  isLoading: boolean;
  isResetting: boolean;
  error: string | null;
  selectedIface: string;
  setSelectedIface: (iface: string) => void;
  resetCounters: (iface?: string) => Promise<boolean>;
  refresh: () => Promise<void>;
}

// =============================================================================
// Rate and Volume Helpers
// =============================================================================

export function formatBps(bps: number): string {
  if (bps < 0 || isNaN(bps)) return "0 bps";
  if (bps >= 1_000_000_000) {
    return `${(bps / 1_000_000_000).toFixed(2)} Gbps`;
  }
  if (bps >= 1_000_000) {
    return `${(bps / 1_000_000).toFixed(2)} Mbps`;
  }
  if (bps >= 1_000) {
    return `${(bps / 1_000).toFixed(1)} Kbps`;
  }
  return `${Math.round(bps)} bps`;
}

// =============================================================================
// Endpoints & Polling Config
// =============================================================================

const PRIMARY_FETCH_ENDPOINT = "/api/v1/monitoring/bandwidth";
const FALLBACK_FETCH_ENDPOINT = "/cgi-bin/quecmanager/monitoring/bandwidth.sh";

const PRIMARY_RESET_ENDPOINT = "/api/v1/monitoring/bandwidth/reset";
const FALLBACK_RESET_ENDPOINT =
  "/cgi-bin/quecmanager/monitoring/bandwidth_reset.sh";

const POLL_INTERVAL_MS = 2000;

export function useBandwidth(defaultIface = "rmnet_data0"): UseBandwidthReturn {
  const [data, setData] = useState<BandwidthSnapshot | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [isResetting, setIsResetting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [selectedIface, setSelectedIface] = useState<string>(defaultIface);

  const activeFetchEndpointRef = useRef<string>(PRIMARY_FETCH_ENDPOINT);
  const inFlightRef = useRef(false);
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const fetchBandwidth = useCallback(async () => {
    if (inFlightRef.current) return;
    inFlightRef.current = true;

    try {
      let response: Response;
      try {
        response = await authFetch(activeFetchEndpointRef.current);
        if (!response.ok && activeFetchEndpointRef.current === PRIMARY_FETCH_ENDPOINT) {
          activeFetchEndpointRef.current = FALLBACK_FETCH_ENDPOINT;
          response = await authFetch(FALLBACK_FETCH_ENDPOINT);
        }
      } catch (err) {
        if (activeFetchEndpointRef.current === PRIMARY_FETCH_ENDPOINT) {
          activeFetchEndpointRef.current = FALLBACK_FETCH_ENDPOINT;
          response = await authFetch(FALLBACK_FETCH_ENDPOINT);
        } else {
          throw err;
        }
      }

      if (!response.ok) {
        throw new Error(`HTTP ${response.status}: ${response.statusText}`);
      }

      const json = await response.json();
      const snapshot: BandwidthSnapshot = json.data ?? json;

      setData(snapshot);
      setError(null);

      // Default interface fallback if user hasn't manually selected another
      if (snapshot.default_interface) {
        setSelectedIface((prev) => {
          if (!prev || (snapshot.interfaces && !snapshot.interfaces[prev])) {
            return snapshot.default_interface;
          }
          return prev;
        });
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load bandwidth");
    } finally {
      setIsLoading(false);
      inFlightRef.current = false;
    }
  }, []);

  const refresh = useCallback(async () => {
    await fetchBandwidth();
  }, [fetchBandwidth]);

  const resetCounters = useCallback(
    async (iface?: string): Promise<boolean> => {
      setIsResetting(true);
      try {
        const targetIface = iface || selectedIface;
        const payload = targetIface ? { interface: targetIface } : {};

        let response: Response;
        try {
          response = await authFetch(PRIMARY_RESET_ENDPOINT, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(payload),
          });
          if (!response.ok) {
            response = await authFetch(FALLBACK_RESET_ENDPOINT, {
              method: "POST",
              headers: { "Content-Type": "application/json" },
              body: JSON.stringify(payload),
            });
          }
        } catch {
          response = await authFetch(FALLBACK_RESET_ENDPOINT, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(payload),
          });
        }

        if (response.ok) {
          await fetchBandwidth();
          return true;
        }
        return false;
      } catch {
        return false;
      } finally {
        setIsResetting(false);
      }
    },
    [fetchBandwidth, selectedIface]
  );

  useEffect(() => {
    let isSubscribed = true;

    const startTimer = () => {
      if (timerRef.current) clearInterval(timerRef.current);
      timerRef.current = setInterval(() => {
        if (!document.hidden && isSubscribed) {
          void fetchBandwidth();
        }
      }, POLL_INTERVAL_MS);
    };

    const stopTimer = () => {
      if (timerRef.current) {
        clearInterval(timerRef.current);
        timerRef.current = null;
      }
    };

    const handleVisibilityChange = () => {
      if (document.hidden) {
        stopTimer();
      } else {
        void fetchBandwidth();
        startTimer();
      }
    };

    void fetchBandwidth();
    if (typeof document !== "undefined") {
      document.addEventListener("visibilitychange", handleVisibilityChange);
      if (!document.hidden) {
        startTimer();
      }
    }

    return () => {
      isSubscribed = false;
      stopTimer();
      if (typeof document !== "undefined") {
        document.removeEventListener("visibilitychange", handleVisibilityChange);
      }
    };
  }, [fetchBandwidth]);

  const interfaceNames = data?.interfaces ? Object.keys(data.interfaces) : [];
  const currentIface =
    (data?.interfaces && data.interfaces[selectedIface]) ||
    (data?.default_interface && data.interfaces[data.default_interface]) ||
    (interfaceNames.length > 0 ? data?.interfaces[interfaceNames[0]] : null) ||
    null;

  return {
    data,
    currentIface,
    interfaceNames,
    isLoading,
    isResetting,
    error,
    selectedIface,
    setSelectedIface,
    resetCounters,
    refresh,
  };
}
