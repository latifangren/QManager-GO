"use client";

import { useCallback, useState, useEffect } from "react";
import type { ChangeEvent, FormEvent } from "react";
import { toast } from "sonner";
import { CircleAlertIcon, EyeIcon, EyeOffIcon, KeyRoundIcon, TerminalIcon, ShieldCheckIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

import { changeSSHPassword } from "@/hooks/use-auth";
import { useSSHSettings } from "@/hooks/use-ssh-settings";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from "@/components/ui/input-group";
import { Switch } from "@/components/ui/switch";
import { SaveButton, useSaveFlash } from "@/components/ui/save-button";
import { cn } from "@/lib/utils";

import {
  CARD_BODY,
  CARD_DESC,
  CARD_PAD,
  CARD_SHELL,
  CARD_TITLE,
  COARSE_TARGET,
  FIELD,
  GROUP_FILL,
  NOTICE,
  PILL_ACTION,
  ROW,
  ROW_GROUP,
} from "./shapes";

const K = "ssh";

const STACK_ROW = "flex flex-col gap-2.5 rounded-field px-4 py-4";
const FIELD_GROUP = cn(FIELD, "px-0 @2xl/card:w-full");
const FIELD_TOGGLE = `rounded-pill text-on-surface-variant hover:text-on-surface ${COARSE_TARGET}`;

interface PasswordFieldProps {
  id: string;
  label: string;
  consequence?: string;
  value: string;
  onChange: (value: string) => void;
  disabled: boolean;
  autoComplete: string;
}

function PasswordField({
  id,
  label,
  consequence,
  value,
  onChange,
  disabled,
  autoComplete,
}: PasswordFieldProps) {
  const { t } = useTranslation("system-settings");
  const [visible, setVisible] = useState(false);
  const Glyph = visible ? EyeOffIcon : EyeIcon;

  return (
    <div className={STACK_ROW}>
      <div className={ROW.TEXT}>
        <label htmlFor={id} className={ROW.LABEL}>
          {label}
        </label>
        {consequence ? (
          <span className={ROW.CONSEQUENCE}>{consequence}</span>
        ) : null}
      </div>

      <InputGroup className={FIELD_GROUP}>
        <InputGroupInput
          id={id}
          type={visible ? "text" : "password"}
          autoComplete={autoComplete}
          value={value}
          onChange={(e: ChangeEvent<HTMLInputElement>) => onChange(e.target.value)}
          disabled={disabled}
          placeholder="••••••••"
          className="rounded-field border-none bg-transparent px-4 font-mono text-body text-on-surface placeholder:text-on-surface-variant/40 focus-visible:ring-0"
        />
        <InputGroupAddon align="inline-end" className="pr-1.5">
          <InputGroupButton
            type="button"
            onClick={() => setVisible((prev) => !prev)}
            disabled={disabled}
            aria-label={visible ? t(`${K}.toggle.hide`) : t(`${K}.toggle.show`)}
            className={FIELD_TOGGLE}
          >
            <Glyph className="size-4" aria-hidden />
          </InputGroupButton>
        </InputGroupAddon>
      </InputGroup>
    </div>
  );
}

export default function SSHPasswordCard() {
  const { t } = useTranslation("system-settings");
  const { saved, markSaved } = useSaveFlash();

  // Daemon settings hook
  const { settings, saving: daemonSaving, updateSettings } = useSSHSettings();
  const [enabled, setEnabled] = useState(settings.enabled);
  const [port, setPort] = useState(String(settings.port));
  const [authorizedKeys, setAuthorizedKeys] = useState(settings.authorized_keys);
  const [daemonDirty, setDaemonDirty] = useState(false);

  useEffect(() => {
    setEnabled(settings.enabled);
    setPort(String(settings.port));
    setAuthorizedKeys(settings.authorized_keys);
    setDaemonDirty(false);
  }, [settings]);

  // Password change form state
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const handleSaveDaemon = async () => {
    const p = parseInt(port, 10);
    if (isNaN(p) || p <= 0 || p > 65535) {
      toast.error("Invalid SSH port (1-65535)");
      return;
    }
    const ok = await updateSettings({
      enabled,
      port: p,
      authorized_keys: authorizedKeys,
    });
    if (ok) {
      setDaemonDirty(false);
    }
  };

  const handlePasswordSubmit = useCallback(
    async (e: FormEvent) => {
      e.preventDefault();
      setError(null);

      if (newPassword.length < 6) {
        setError(t(`${K}.errors.too_short`));
        return;
      }
      if (newPassword !== confirmPassword) {
        setError(t(`${K}.errors.mismatch`));
        return;
      }

      setSubmitting(true);
      try {
        const result = await changeSSHPassword(currentPassword, newPassword, confirmPassword);
        if (result.success) {
          setCurrentPassword("");
          setNewPassword("");
          setConfirmPassword("");
          markSaved();
          toast.success(t(`${K}.toast_saved`));
        } else {
          const msg = result.error || t(`${K}.errors.failed`);
          setError(msg);
          toast.error(msg);
        }
      } catch {
        const msg = t(`${K}.errors.failed`);
        setError(msg);
        toast.error(msg);
      } finally {
        setSubmitting(false);
      }
    },
    [currentPassword, newPassword, confirmPassword, t, markSaved]
  );

  return (
    <Card className={CARD_SHELL}>
      <CardHeader className={CARD_PAD}>
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <TerminalIcon className="size-5 text-primary" />
            <CardTitle className={CARD_TITLE}>{t(`${K}.card.title`)}</CardTitle>
          </div>
          <div className="flex items-center gap-2">
            {settings.conflict ? (
              <span className="rounded-full bg-error-container px-2.5 py-0.5 text-caption font-medium text-on-error-container">
                PORT CONFLICT
              </span>
            ) : (
              <span className="text-caption text-on-surface-variant font-mono">
                {settings.running ? "ACTIVE : " + settings.port : "INACTIVE"}
              </span>
            )}
          </div>
        </div>
        <CardDescription className={CARD_DESC}>
          {t(`${K}.card.description`)}
        </CardDescription>
      </CardHeader>

      <CardContent className={cn(CARD_PAD, CARD_BODY, "space-y-6")}>
        {settings.conflict && (
          <div className={cn(NOTICE.BOX, NOTICE.FAILED)} role="alert">
            <CircleAlertIcon className={NOTICE.GLYPH} aria-hidden />
            <p className={NOTICE.TEXT}>
              {settings.conflict_msg ||
                `Port ${settings.port} is already used by an external daemon (e.g. Dropbear). Please change the port or disable the external SSH daemon.`}
            </p>
          </div>
        )}

        {/* 1. SSH Server Daemon Controls */}
        <div className="space-y-3">
          <div className="flex items-center gap-2 text-label text-on-surface font-medium">
            <ShieldCheckIcon className="size-4 text-primary" />
            <span>SSH Server Daemon</span>
          </div>

          <div className={ROW_GROUP}>
            {/* Toggle Enable */}
            <div className={cn(ROW.ROOT, "justify-between items-center")}>
              <div className={ROW.TEXT}>
                <span className={ROW.LABEL}>Enable Native SSH Server</span>
                <span className={ROW.CONSEQUENCE}>
                  Runs pure Go standalone SSH daemon on this device.
                </span>
              </div>
              <Switch
                checked={enabled}
                onCheckedChange={(checked) => {
                  setEnabled(checked);
                  setDaemonDirty(true);
                }}
              />
            </div>

            {/* Port Setting */}
            <div className={cn(ROW.ROOT, "justify-between items-center")}>
              <div className={ROW.TEXT}>
                <span className={ROW.LABEL}>SSH Port</span>
                <span className={ROW.CONSEQUENCE}>
                  Default is 22. Valid range 1 - 65535.
                </span>
              </div>
              <input
                type="number"
                min={1}
                max={65535}
                value={port}
                onChange={(e) => {
                  setPort(e.target.value);
                  setDaemonDirty(true);
                }}
                disabled={!enabled}
                className={cn(
                  FIELD,
                  "w-28 text-center font-mono text-body text-on-surface placeholder:text-on-surface-variant/40"
                )}
              />
            </div>

            {/* Authorized Public Keys */}
            <div className={STACK_ROW}>
              <div className={ROW.TEXT}>
                <span className={ROW.LABEL}>Authorized SSH Public Keys</span>
                <span className={ROW.CONSEQUENCE}>
                  Paste public keys (one per line, e.g. ssh-ed25519 ...) for passwordless login.
                </span>
              </div>
              <textarea
                rows={3}
                value={authorizedKeys}
                onChange={(e) => {
                  setAuthorizedKeys(e.target.value);
                  setDaemonDirty(true);
                }}
                disabled={!enabled}
                placeholder="ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA... user@machine"
                className="w-full rounded-field border-none bg-surface-container-high/40 p-3 font-mono text-caption text-on-surface placeholder:text-on-surface-variant/40 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-primary/60"
              />
            </div>
          </div>

          {daemonDirty && (
            <div className="flex justify-end pt-1">
              <button
                type="button"
                onClick={handleSaveDaemon}
                disabled={daemonSaving}
                className={cn(
                  PILL_ACTION,
                  "bg-primary text-on-primary hover:bg-primary/90 px-4 py-2 font-medium"
                )}
              >
                {daemonSaving ? "Applying..." : "Apply SSH Daemon Settings"}
              </button>
            </div>
          )}
        </div>

        {/* 2. Root SSH Password Change Form */}
        <form onSubmit={handlePasswordSubmit} className="space-y-4 pt-4 border-t border-outline/10">
          <div className="flex items-center gap-2 text-label text-on-surface font-medium">
            <KeyRoundIcon className="size-4 text-primary" />
            <span>Root Password Authentication</span>
          </div>

          <div className={ROW_GROUP}>
            <PasswordField
              id="ssh-current-password"
              label={t(`${K}.fields.current.label`)}
              consequence={t(`${K}.fields.current.consequence`)}
              value={currentPassword}
              onChange={setCurrentPassword}
              disabled={submitting}
              autoComplete="current-password"
            />

            <PasswordField
              id="ssh-new-password"
              label={t(`${K}.fields.new.label`)}
              consequence={t(`${K}.fields.new.consequence`)}
              value={newPassword}
              onChange={setNewPassword}
              disabled={submitting}
              autoComplete="new-password"
            />

            <PasswordField
              id="ssh-confirm-password"
              label={t(`${K}.fields.confirm.label`)}
              value={confirmPassword}
              onChange={setConfirmPassword}
              disabled={submitting}
              autoComplete="new-password"
            />
          </div>

          {error ? (
            <div className={cn(NOTICE.BOX, NOTICE.FAILED)} role="alert">
              <CircleAlertIcon className={NOTICE.GLYPH} aria-hidden />
              <p className={NOTICE.TEXT}>{error}</p>
            </div>
          ) : null}

          <div className={cn(GROUP_FILL, "justify-end")}>
            <SaveButton
              isSaving={submitting}
              saved={saved}
              label={t(`${K}.card.save`)}
              disabled={submitting || !currentPassword || !newPassword || !confirmPassword}
              className={PILL_ACTION}
            />
          </div>
        </form>
      </CardContent>
    </Card>
  );
}
