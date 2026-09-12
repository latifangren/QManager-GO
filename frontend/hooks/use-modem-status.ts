"use client";

import { useState, useEffect, useCallback } from "react";
import { authFetch } from "@/lib/auth-fetch";
import type { ModemStatus } from "@/types/modem-status";

// =============================================================================
// useModemStatus — Polling Hook for QManager Dashboard
// =============================================================================
// Fetches the cached modem status JSON from the CGI endpoint at a regular
// interval. Shares a single polling loop across all subscribers to eliminate
// duplicate requests and reduce CPU strain on the modem.
//
// Usage:
//   const { data, isLoading, isStale, receivedAtMs, error, refresh } =
//     useModemStatus();
//
// The hook does NOT touch the modem — it only reads the pre-built JSON cache.
// =============================================================================

/** How often to poll the CGI endpoint (ms) */
const DEFAULT_POLL_INTERVAL = 2000;

/** After this many seconds without a fresh timestamp, data is "stale" */
const STALE_THRESHOLD_SECONDS = 10;

/** CGI endpoint path (proxied in dev via next.config.ts rewrites) */
const FETCH_ENDPOINT = "/cgi-bin/quecmanager/at_cmd/fetch_data.sh";

export interface UseModemStatusOptions {
  /** Polling interval in ms (default: 2000) */
  pollInterval?: number;
  /** Whether polling is active (default: true) */
  enabled?: boolean;
}

export interface UseModemStatusReturn {
  /** The latest modem status data (null before first successful fetch) */
  data: ModemStatus | null;
  /** True during the very first fetch (before any data is available) */
  isLoading: boolean;
  /** True if the data's timestamp is older than the stale threshold */
  isStale: boolean;
  /**
   * Browser wall-clock ms at the moment the current `data`
   * snapshot was received by the client. `null` until the first successful fetch.
   */
  receivedAtMs: number | null;
  /** Error message if the last fetch failed */
  error: string | null;
  /** Manually trigger an immediate refresh */
  refresh: () => void;
}

// =============================================================================
// Module-level Singleton Polling Manager
// =============================================================================

interface SharedState {
  data: ModemStatus | null;
  isLoading: boolean;
  isStale: boolean;
  receivedAtMs: number | null;
  error: string | null;
}

let sharedState: SharedState = {
  data: null,
  isLoading: true,
  isStale: false,
  receivedAtMs: null,
  error: null,
};

type Listener = (state: SharedState) => void;
const listeners = new Set<Listener>();

let pollTimer: ReturnType<typeof setInterval> | null = null;
let inFlight = false;

function notifyListeners() {
  for (const listener of listeners) {
    listener(sharedState);
  }
}

async function fetchStatusShared() {
  if (inFlight) return;
  inFlight = true;

  try {
    const response = await authFetch(FETCH_ENDPOINT);

    if (!response.ok) {
      throw new Error(`HTTP ${response.status}: ${response.statusText}`);
    }

    const json: ModemStatus = await response.json();

    const nowMs = Date.now();
    let isStale = false;
    if (json.timestamp) {
      const ageSeconds = Math.floor(nowMs / 1000) - json.timestamp;
      isStale = ageSeconds > STALE_THRESHOLD_SECONDS;
    }

    sharedState = {
      data: json,
      isLoading: false,
      isStale,
      receivedAtMs: nowMs,
      error: null,
    };
  } catch (err) {
    sharedState = {
      ...sharedState,
      isLoading: false,
      error: err instanceof Error ? err.message : "Failed to fetch status",
    };
  } finally {
    inFlight = false;
    notifyListeners();
  }
}

function startSharedPolling(intervalMs: number = DEFAULT_POLL_INTERVAL) {
  if (pollTimer) return;
  pollTimer = setInterval(() => {
    if (typeof document !== "undefined" && document.hidden) return;
    fetchStatusShared();
  }, intervalMs);
}

function stopSharedPolling() {
  if (pollTimer) {
    clearInterval(pollTimer);
    pollTimer = null;
  }
}

function handleVisibilityChange() {
  if (typeof document === "undefined") return;
  if (document.hidden) {
    stopSharedPolling();
  } else {
    fetchStatusShared();
    startSharedPolling();
  }
}

export function useModemStatus(
  options: UseModemStatusOptions = {}
): UseModemStatusReturn {
  const { pollInterval = DEFAULT_POLL_INTERVAL, enabled = true } = options;
  const [state, setState] = useState<SharedState>(sharedState);

  useEffect(() => {
    if (!enabled) return;

    const listener: Listener = (newState) => {
      setState(newState);
    };
    listeners.add(listener);

    if (listeners.size === 1) {
      if (typeof document !== "undefined") {
        document.addEventListener("visibilitychange", handleVisibilityChange);
      }
      if (typeof document === "undefined" || !document.hidden) {
        fetchStatusShared();
        startSharedPolling(pollInterval);
      }
    } else {
      setState(sharedState);
    }

    return () => {
      listeners.delete(listener);
      if (listeners.size === 0) {
        stopSharedPolling();
        if (typeof document !== "undefined") {
          document.removeEventListener("visibilitychange", handleVisibilityChange);
        }
      }
    };
  }, [enabled, pollInterval]);

  const refresh = useCallback(() => {
    fetchStatusShared();
  }, []);

  return {
    data: state.data,
    isLoading: state.isLoading,
    isStale: state.isStale,
    receivedAtMs: state.receivedAtMs,
    error: state.error,
    refresh,
  };
}
