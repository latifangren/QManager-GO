"use client";

import { useState, useCallback, useEffect } from "react";
import { toast } from "sonner";
import { authFetch } from "@/lib/auth-fetch";

export interface SSHSettings {
  enabled: boolean;
  port: number;
  running: boolean;
  conflict?: boolean;
  conflict_msg?: string;
  authorized_keys: string;
}

export interface UseSSHSettingsReturn {
  settings: SSHSettings;
  loading: boolean;
  saving: boolean;
  reload: () => Promise<void>;
  updateSettings: (payload: {
    enabled?: boolean;
    port?: number;
    authorized_keys?: string;
  }) => Promise<boolean>;
}

const API_ENDPOINT = "/api/v1/system/ssh";
const CGI_ENDPOINT = "/cgi-bin/quecmanager/system/ssh.sh";

export function useSSHSettings(): UseSSHSettingsReturn {
  const [settings, setSettings] = useState<SSHSettings>({
    enabled: true,
    port: 22,
    running: true,
    authorized_keys: "",
  });
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  const fetchSettings = useCallback(async () => {
    try {
      let resp = await authFetch(API_ENDPOINT);
      if (!resp.ok) {
        resp = await authFetch(CGI_ENDPOINT);
      }
      if (resp.ok) {
        const json = await resp.json();
        const data = json.data || json;
        setSettings({
          enabled: data.enabled ?? true,
          port: data.port ?? 22,
          running: data.running ?? true,
          conflict: data.conflict ?? false,
          conflict_msg: data.conflict_msg ?? "",
          authorized_keys: data.authorized_keys ?? "",
        });
      }
    } catch {
      // Keep defaults
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchSettings();
  }, [fetchSettings]);

  const updateSettings = useCallback(
    async (payload: {
      enabled?: boolean;
      port?: number;
      authorized_keys?: string;
    }): Promise<boolean> => {
      setSaving(true);
      try {
        const fullPayload = {
          enabled: payload.enabled ?? settings.enabled,
          port: payload.port ?? settings.port,
          authorized_keys: payload.authorized_keys ?? settings.authorized_keys,
        };

        let resp = await authFetch(API_ENDPOINT, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(fullPayload),
        });

        if (!resp.ok) {
          resp = await authFetch(CGI_ENDPOINT, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(fullPayload),
          });
        }

        if (resp.ok) {
          const json = await resp.json();
          const data = json.data || fullPayload;
          setSettings({
            enabled: data.enabled ?? fullPayload.enabled,
            port: data.port ?? fullPayload.port,
            running: data.running ?? (data.conflict ? false : fullPayload.enabled),
            conflict: data.conflict ?? false,
            conflict_msg: data.conflict_msg ?? "",
            authorized_keys: data.authorized_keys ?? fullPayload.authorized_keys,
          });
          toast.success("SSH daemon settings updated successfully.");
          return true;
        } else {
          toast.error("Failed to update SSH settings.");
          return false;
        }
      } catch {
        toast.error("Network error while updating SSH settings.");
        return false;
      } finally {
        setSaving(false);
      }
    },
    [settings]
  );

  return {
    settings,
    loading,
    saving,
    reload: fetchSettings,
    updateSettings,
  };
}
