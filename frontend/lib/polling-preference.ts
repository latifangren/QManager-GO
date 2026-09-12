// =============================================================================
// polling-preference — telemetry polling modes and intervals
// =============================================================================
// Provides adaptive polling intervals ('active', 'balanced', 'low_power')
// for telemetry queries, reducing modem CPU load and thermal generation.
// Persisted in localStorage and synchronised across tabs and components.
// =============================================================================

export type PollingMode = "active" | "balanced" | "low_power";

export const POLLING_STORAGE_KEY = "qm_polling_mode";

export const POLLING_INTERVALS: Record<PollingMode, number> = {
  active: 1000,
  balanced: 2000,
  low_power: 5000,
};

const POLLING_CHANGE_EVENT = "qm_polling_mode_change";

export function isPollingMode(value: unknown): value is PollingMode {
  return value === "active" || value === "balanced" || value === "low_power";
}

/**
 * Read the current polling mode preference (SSR-safe).
 * Defaults to 'balanced' (2000ms).
 */
export function getPollingMode(): PollingMode {
  if (typeof window === "undefined") return "balanced";
  try {
    const stored = window.localStorage.getItem(POLLING_STORAGE_KEY);
    return isPollingMode(stored) ? stored : "balanced";
  } catch {
    return "balanced";
  }
}

/**
 * Returns the millisecond interval for a given mode (or the currently stored mode).
 */
export function getPollingInterval(mode?: PollingMode): number {
  const m = mode || getPollingMode();
  return POLLING_INTERVALS[m] || POLLING_INTERVALS.balanced;
}

/**
 * Persist the polling mode choice and dispatch an event to active subscribers.
 */
export function setPollingMode(mode: PollingMode): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(POLLING_STORAGE_KEY, mode);
  } catch {
    // Non-fatal
  }
  window.dispatchEvent(new CustomEvent(POLLING_CHANGE_EVENT, { detail: mode }));
}

/**
 * Subscribe to polling mode changes across the current window and storage events.
 */
export function subscribePollingMode(callback: (mode: PollingMode) => void): () => void {
  if (typeof window === "undefined") {
    return () => {};
  }

  const handleCustomEvent = (event: Event) => {
    const customEvent = event as CustomEvent<PollingMode>;
    if (customEvent.detail && isPollingMode(customEvent.detail)) {
      callback(customEvent.detail);
    }
  };

  const handleStorage = (event: StorageEvent) => {
    if (event.key === POLLING_STORAGE_KEY && isPollingMode(event.newValue)) {
      callback(event.newValue);
    }
  };

  window.addEventListener(POLLING_CHANGE_EVENT, handleCustomEvent);
  window.addEventListener("storage", handleStorage);

  return () => {
    window.removeEventListener(POLLING_CHANGE_EVENT, handleCustomEvent);
    window.removeEventListener("storage", handleStorage);
  };
}
