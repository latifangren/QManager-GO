"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import { authFetch } from "@/lib/auth-fetch";
import type { ModemSubsysData } from "@/types/modem-subsys";

const POLL_INTERVAL = 2000;
const FETCH_ENDPOINT = "/cgi-bin/quecmanager/system/modem-subsys.sh";

export interface UseModemSubsysReturn {
  data: ModemSubsysData | null;
  isLoading: boolean;
  error: string | null;
  refetch: () => void;
}

export function useModemSubsys(): UseModemSubsysReturn {
  const [data, setData] = useState<ModemSubsysData | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const mountedRef = useRef(true);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const inFlightRef = useRef(false);

  const fetchData = useCallback(async () => {
    // Skip when a prior request is still in-flight — prevents request pile-up
    // at 1Hz polling if the device is briefly slow.
    if (inFlightRef.current) return;
    inFlightRef.current = true;
    try {
      const response = await authFetch(FETCH_ENDPOINT);

      if (!response.ok) {
        throw new Error(`HTTP ${response.status}: ${response.statusText}`);
      }

      const json: ModemSubsysData = await response.json();

      if (!mountedRef.current) return;

      setData(json);
      setError(null);
      setIsLoading(false);
    } catch (err) {
      if (!mountedRef.current) return;

      setError(
        err instanceof Error ? err.message : "Failed to fetch modem subsystem status"
      );
      // Retain stale data so the card doesn't blank out on transient failure.
      setIsLoading(false);
    } finally {
      inFlightRef.current = false;
    }
  }, []);

  const refetch = useCallback(() => {
    void fetchData();
  }, [fetchData]);

  useEffect(() => {
    mountedRef.current = true;

    void fetchData();
    intervalRef.current = setInterval(() => void fetchData(), POLL_INTERVAL);

    return () => {
      mountedRef.current = false;
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
        intervalRef.current = null;
      }
    };
  }, [fetchData]);

  return { data, isLoading, error, refetch };
}
