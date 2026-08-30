"use client";

import { useState, useCallback, useRef, useEffect } from "react";
import { authFetch } from "@/lib/auth-fetch";

// =============================================================================
// useMtuSettings — One-Shot MTU Fetch & Save Hook
// =============================================================================
// Fetches current MTU and enabled status on mount.
// Provides saveMtu for applying new value, and disableMtu for removing it.
//
// Backend endpoint:
//   GET/POST /cgi-bin/quecmanager/network/mtu.sh
// =============================================================================

const CGI_ENDPOINT = "/cgi-bin/quecmanager/network/mtu.sh";

export interface MtuSettingsData {
  /** Whether custom MTU is currently active */
  isEnabled: boolean;
  /** Current MTU value on the interface */
  currentValue: number;
}

export interface UseMtuSettingsReturn {
  /** Current MTU data (null before first fetch) */
  data: MtuSettingsData | null;
  /** True while initial fetch is in progress */
  isLoading: boolean;
  /** True while a save operation is in progress */
  isSaving: boolean;
  /** Error message if fetch or save failed */
  error: string | null;
  /** Apply a new MTU value. Returns true on success. */
  saveMtu: (mtu: number) => Promise<boolean>;
  /** Disable custom MTU (revert to default). Returns true on success. */
  disableMtu: () => Promise<boolean>;
  /** Re-fetch MTU data */
  refresh: () => void;
}

export function useMtuSettings(): UseMtuSettingsReturn {
  const [data, setData] = useState<MtuSettingsData | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [isSaving, setIsSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  // ---------------------------------------------------------------------------
  // Fetch current MTU status
  // ---------------------------------------------------------------------------
  const fetchMtu = useCallback(async (silent = false) => {
    if (!silent) setIsLoading(true);
    setError(null);

    try {
      const resp = await authFetch(CGI_ENDPOINT);
      if (!resp.ok) {
        throw new Error(`HTTP ${resp.status}: ${resp.statusText}`);
      }

      const json = await resp.json();
      if (!mountedRef.current) return;

      if (!json.success) {
        setError(json.error || "Failed to fetch MTU settings");
        return;
      }

      setData({
        isEnabled: json.is_enabled,
        currentValue: json.current_value,
      });
    } catch (err) {
      if (!mountedRef.current) return;
      setError(
        err instanceof Error ? err.message : "Failed to fetch MTU settings",
      );
    } finally {
      if (mountedRef.current && !silent) {
        setIsLoading(false);
      }
    }
  }, []);

  // Fetch on mount
  useEffect(() => {
    fetchMtu();
  }, [fetchMtu]);

  // ---------------------------------------------------------------------------
  // Save new MTU value
  // ---------------------------------------------------------------------------
  const saveMtu = useCallback(
    async (mtu: number): Promise<boolean> => {
      setError(null);
      setIsSaving(true);

      try {
        const resp = await authFetch(CGI_ENDPOINT, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ mtu }),
        });

        if (!resp.ok) {
          throw new Error(`HTTP ${resp.status}: ${resp.statusText}`);
        }

        const json = await resp.json();
        if (!mountedRef.current) return false;

        if (!json.success) {
          setError(json.detail || json.error || "Failed to apply MTU");
          return false;
        }

        // Silent re-fetch to update local state
        await fetchMtu(true);
        return true;
      } catch (err) {
        if (!mountedRef.current) return false;
        setError(err instanceof Error ? err.message : "Failed to apply MTU");
        return false;
      } finally {
        if (mountedRef.current) {
          setIsSaving(false);
        }
      }
    },
    [fetchMtu],
  );

  // ---------------------------------------------------------------------------
  // Disable custom MTU
  // ---------------------------------------------------------------------------
  const disableMtu = useCallback(async (): Promise<boolean> => {
    setError(null);
    setIsSaving(true);

    try {
      const resp = await authFetch(CGI_ENDPOINT, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ mtu: "disable" }),
      });

      if (!resp.ok) {
        throw new Error(`HTTP ${resp.status}: ${resp.statusText}`);
      }

      const json = await resp.json();
      if (!mountedRef.current) return false;

      if (!json.success) {
        setError(json.detail || json.error || "Failed to disable MTU");
        return false;
      }

      // Silent re-fetch to update local state
      await fetchMtu(true);
      return true;
    } catch (err) {
      if (!mountedRef.current) return false;
      setError(err instanceof Error ? err.message : "Failed to disable MTU");
      return false;
    } finally {
      if (mountedRef.current) {
        setIsSaving(false);
      }
    }
  }, [fetchMtu]);

  return {
    data,
    isLoading,
    isSaving,
    error,
    saveMtu,
    disableMtu,
    refresh: fetchMtu,
  };
}
