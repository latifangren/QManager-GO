"use client";

import { useState, useCallback, useRef, useEffect } from "react";
import { authFetch } from "@/lib/auth-fetch";

// =============================================================================
// useTtlSettings — One-Shot TTL/HL Fetch & Save Hook
// =============================================================================
// Fetches current TTL and HL values on mount.
// Provides saveTtlHl for applying new values.
//
// Backend endpoint:
//   GET/POST /cgi-bin/quecmanager/network/ttl.sh
// =============================================================================

const CGI_ENDPOINT = "/cgi-bin/quecmanager/network/ttl.sh";

export interface TtlSettingsData {
  /** Whether custom TTL/HL is currently active */
  isEnabled: boolean;
  /** Current TTL value (0 = default / disabled) */
  ttl: number;
  /** Current HL value (0 = default / disabled) */
  hl: number;
  /** Whether TTL/HL is set to autostart on boot */
  autostart: boolean;
}

export interface UseTtlSettingsReturn {
  /** Current TTL/HL data (null before first fetch) */
  data: TtlSettingsData | null;
  /** True while initial fetch is in progress */
  isLoading: boolean;
  /** True while a save operation is in progress */
  isSaving: boolean;
  /** Error message if fetch or save failed */
  error: string | null;
  /** Apply new TTL/HL values. Returns true on success. */
  saveTtlHl: (ttl: number, hl: number) => Promise<boolean>;
  /** Re-fetch TTL/HL data */
  refresh: () => void;
}

export function useTtlSettings(): UseTtlSettingsReturn {
  const [data, setData] = useState<TtlSettingsData | null>(null);
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
  // Fetch current TTL/HL values
  // ---------------------------------------------------------------------------
  const fetchTtl = useCallback(async (silent = false) => {
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
        setError(json.error || "Failed to fetch TTL settings");
        return;
      }

      setData({
        isEnabled: json.is_enabled,
        ttl: json.ttl,
        hl: json.hl,
        autostart: json.autostart,
      });
    } catch (err) {
      if (!mountedRef.current) return;
      setError(
        err instanceof Error ? err.message : "Failed to fetch TTL settings",
      );
    } finally {
      if (mountedRef.current && !silent) {
        setIsLoading(false);
      }
    }
  }, []);

  // Fetch on mount
  useEffect(() => {
    fetchTtl();
  }, [fetchTtl]);

  // ---------------------------------------------------------------------------
  // Save TTL/HL values
  // ---------------------------------------------------------------------------
  const saveTtlHl = useCallback(
    async (ttl: number, hl: number): Promise<boolean> => {
      setError(null);
      setIsSaving(true);

      try {
        const resp = await authFetch(CGI_ENDPOINT, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ ttl, hl }),
        });

        if (!resp.ok) {
          throw new Error(`HTTP ${resp.status}: ${resp.statusText}`);
        }

        const json = await resp.json();
        if (!mountedRef.current) return false;

        if (!json.success) {
          setError(json.detail || json.error || "Failed to apply TTL/HL");
          return false;
        }

        // Silent re-fetch to update local state
        await fetchTtl(true);
        return true;
      } catch (err) {
        if (!mountedRef.current) return false;
        setError(err instanceof Error ? err.message : "Failed to apply TTL/HL");
        return false;
      } finally {
        if (mountedRef.current) {
          setIsSaving(false);
        }
      }
    },
    [fetchTtl],
  );

  return {
    data,
    isLoading,
    isSaving,
    error,
    saveTtlHl,
    refresh: fetchTtl,
  };
}
