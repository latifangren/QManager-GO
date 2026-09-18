"use client";

import { useState, useEffect, useCallback } from "react";
import { authFetch } from "@/lib/auth-fetch";
import { getPollingInterval, subscribePollingMode } from "@/lib/polling-preference";
import type { ModemStatus } from "@/types/modem-status";

// =============================================================================
// useModemStatus — Real-time SSE & Polling Hook for QManager Dashboard
// =============================================================================
// Streams real-time telemetry from SSE endpoint (/api/v1/telemetry/stream)
// with automatic fallback to HTTP polling. Shares a single connection/timer
// across all components to eliminate duplicate requests and minimize modem load.
//
// Usage:
//   const { data, isLoading, isStale, receivedAtMs, error, refresh } =
//     useModemStatus();
//
// The hook does NOT touch the modem — it only reads cached in-memory telemetry.
// =============================================================================

/** How often to poll the CGI endpoint (ms) as baseline fallback */
const DEFAULT_POLL_INTERVAL = 2000;

/** After this many seconds without a fresh timestamp, data is "stale" */
const STALE_THRESHOLD_SECONDS = 10;

/** HTTP endpoint path */
const FETCH_ENDPOINT = "/cgi-bin/quecmanager/at_cmd/fetch_data.sh";

/** SSE stream endpoints (primary REST, fallback CGI) */
const SSE_PRIMARY_ENDPOINT = "/api/v1/telemetry/stream";
const SSE_FALLBACK_ENDPOINT = "/cgi-bin/quecmanager/api/stream/status";

export interface UseModemStatusOptions {
  /** Polling interval in ms (optional override) */
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
// Module-level Singleton State & Stream Manager
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
let eventSource: EventSource | null = null;
let isSseConnected = false;
let sseEndpoint = SSE_PRIMARY_ENDPOINT;
let inFlight = false;
let activePollInterval: number = DEFAULT_POLL_INTERVAL;
let pollingUnsubscribe: (() => void) | null = null;

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

function startSharedPolling(intervalMs?: number) {
  if (intervalMs) {
    activePollInterval = intervalMs;
  }
  if (pollTimer) return;
  pollTimer = setInterval(() => {
    if (typeof document !== "undefined" && document.hidden) return;
    fetchStatusShared();
  }, activePollInterval);
}

function stopSharedPolling() {
  if (pollTimer) {
    clearInterval(pollTimer);
    pollTimer = null;
  }
}

function connectSSE() {
  if (typeof window === "undefined" || !("EventSource" in window)) {
    return;
  }
  if (typeof document !== "undefined" && document.hidden) {
    return;
  }
  if (eventSource) {
    disconnectSSE();
  }

  try {
    const es = new EventSource(sseEndpoint);
    eventSource = es;

    es.onopen = () => {
      isSseConnected = true;
      // Pause HTTP polling while SSE stream is active
      stopSharedPolling();
    };

    es.onmessage = (event) => {
      try {
        if (!event.data || event.data.startsWith(":")) return;
        const json: ModemStatus = JSON.parse(event.data);

        let isStale = false;
        if (json.timestamp) {
          const modemTime = new Date(json.timestamp).getTime();
          if (!isNaN(modemTime)) {
            const ageSeconds = (Date.now() - modemTime) / 1000;
            isStale = ageSeconds > STALE_THRESHOLD_SECONDS;
          }
        }

        sharedState = {
          data: json,
          isLoading: false,
          isStale,
          receivedAtMs: Date.now(),
          error: null,
        };
        notifyListeners();
      } catch {
        // Ignore unparseable or heartbeat messages
      }
    };

    es.onerror = () => {
      disconnectSSE();
      // Alternate between primary REST and fallback CGI streaming endpoints
      sseEndpoint =
        sseEndpoint === SSE_PRIMARY_ENDPOINT
          ? SSE_FALLBACK_ENDPOINT
          : SSE_PRIMARY_ENDPOINT;

      // Fallback to HTTP polling
      startSharedPolling(activePollInterval);
    };
  } catch {
    disconnectSSE();
    startSharedPolling(activePollInterval);
  }
}

function disconnectSSE() {
  if (eventSource) {
    eventSource.close();
    eventSource = null;
  }
  isSseConnected = false;
}

function handleVisibilityChange() {
  if (typeof document === "undefined") return;
  if (document.hidden) {
    disconnectSSE();
    stopSharedPolling();
  } else {
    fetchStatusShared();
    connectSSE();
    if (!isSseConnected) {
      startSharedPolling(activePollInterval);
    }
  }
}

export function useModemStatus(
  options: UseModemStatusOptions = {}
): UseModemStatusReturn {
  const { pollInterval, enabled = true } = options;
  const [state, setState] = useState<SharedState>(sharedState);

  useEffect(() => {
    if (!enabled) return;

    const effectiveInterval = pollInterval || getPollingInterval();
    activePollInterval = effectiveInterval;

    const listener: Listener = (newState) => {
      setState(newState);
    };
    listeners.add(listener);

    if (listeners.size === 1) {
      if (typeof document !== "undefined") {
        document.addEventListener("visibilitychange", handleVisibilityChange);
      }

      // Sync interval when user changes polling preference
      pollingUnsubscribe = subscribePollingMode((mode) => {
        const newInterval = getPollingInterval(mode);
        activePollInterval = newInterval;
        if (!isSseConnected && pollTimer) {
          stopSharedPolling();
          startSharedPolling(newInterval);
        }
      });

      if (typeof document === "undefined" || !document.hidden) {
        fetchStatusShared();
        connectSSE();
        if (!isSseConnected) {
          startSharedPolling(effectiveInterval);
        }
      }
    } else {
      queueMicrotask(() => setState(sharedState));
    }

    return () => {
      listeners.delete(listener);
      if (listeners.size === 0) {
        disconnectSSE();
        stopSharedPolling();
        if (pollingUnsubscribe) {
          pollingUnsubscribe();
          pollingUnsubscribe = null;
        }
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
