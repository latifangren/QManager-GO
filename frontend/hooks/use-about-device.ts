"use client";

import { useState, useCallback, useRef, useEffect } from "react";
import { authFetch } from "@/lib/auth-fetch";
import type { AboutDeviceData, AboutDeviceResponse } from "@/types/about-device";

// =============================================================================
// useAboutDevice — One-shot Fetch Hook for About Device Data
// =============================================================================
// Fetches device identity, network addresses, 3GPP release info, and system
// info on mount. No polling — this data is static/semi-static.
//
// Backend: GET /cgi-bin/quecmanager/device/about.sh
// =============================================================================

const CGI_ENDPOINT = "/cgi-bin/quecmanager/device/about.sh";

export interface UseAboutDeviceReturn {
  data: AboutDeviceData | null;
  isLoading: boolean;
  error: string | null;
  refresh: () => void;
}

export function useAboutDevice(): UseAboutDeviceReturn {
  const [data, setData] = useState<AboutDeviceData | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  const fetchData = useCallback(async (silent = false) => {
    if (!silent) setIsLoading(true);
    setError(null);

    try {
      const resp = await authFetch(CGI_ENDPOINT);
      if (!resp.ok) {
        throw new Error(`HTTP ${resp.status}: ${resp.statusText}`);
      }

      const json: AboutDeviceResponse = await resp.json();
      if (!mountedRef.current) return;

      if (!json.success) {
        setError(json.error || "Failed to fetch device information");
        return;
      }

      setData({
        device: json.device,
        threeGppRelease: json["3gpp_release"],
        network: json.network,
        system: json.system,
      });
    } catch (err) {
      if (!mountedRef.current) return;
      setError(
        err instanceof Error
          ? err.message
          : "Failed to fetch device information",
      );
    } finally {
      if (mountedRef.current && !silent) {
        setIsLoading(false);
      }
    }
  }, []);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  const refresh = useCallback(() => {
    fetchData();
  }, [fetchData]);

  return { data, isLoading, error, refresh };
}
