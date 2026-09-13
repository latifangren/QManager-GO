"use client";

import { useCallback, useEffect, useState } from "react";
import { GaugeIcon, MoonIcon, RadioIcon, ZapIcon } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { motion } from "motion/react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  getPollingMode,
  setPollingMode,
  subscribePollingMode,
  type PollingMode,
} from "@/lib/polling-preference";
import { staggerRowItem, staggerRows } from "@/lib/motion";
import { cn } from "@/lib/utils";

import {
  CARD_BODY,
  CARD_DESC,
  CARD_PAD,
  CARD_SHELL,
  CARD_TITLE,
  FOCUS_RING,
  GROUP_FILL,
  ROW_GROUP,
} from "./shapes";

interface PollingOption {
  readonly mode: PollingMode;
  readonly title: string;
  readonly intervalSec: string;
  readonly icon: LucideIcon;
  readonly subtext: string;
}

const POLLING_OPTIONS: readonly PollingOption[] = [
  {
    mode: "active",
    title: "Active (Cepat · 1s)",
    intervalSec: "1s",
    icon: ZapIcon,
    subtext:
      "Real-time 1 Hz update rate. Best for tower positioning and live signal tests.",
  },
  {
    mode: "balanced",
    title: "Balanced (Standar · 2s)",
    intervalSec: "2s",
    icon: GaugeIcon,
    subtext:
      "Default balanced mode. Smooth dashboard monitoring with minimal CPU footprint.",
  },
  {
    mode: "low_power",
    title: "Low Power (Hemat Daya · 5s)",
    intervalSec: "5s",
    icon: MoonIcon,
    subtext:
      "Lightest CPU load (~15% idle). Best for unattended or 24/7 router deployment.",
  },
] as const;

export default function AdaptivePollingCard() {
  const [mode, setMode] = useState<PollingMode>(() => getPollingMode());

  // Synchronise state with cross-tab and storage events
  useEffect(() => {
    return subscribePollingMode((newMode) => {
      setMode(newMode);
    });
  }, []);

  const handleSelect = useCallback(
    (newMode: PollingMode) => {
      if (newMode === mode) return;
      setMode(newMode);
      setPollingMode(newMode);
      const option = POLLING_OPTIONS.find((opt) => opt.mode === newMode);
      const label = option ? option.title : newMode;
      toast.success(`Telemetry polling set to ${label}`);
    },
    [mode]
  );

  const activeOption =
    POLLING_OPTIONS.find((opt) => opt.mode === mode) ?? POLLING_OPTIONS[1];

  return (
    <Card className={CARD_SHELL}>
      <CardHeader className={CARD_PAD}>
        <CardTitle className={CARD_TITLE}>Adaptive Telemetry Polling</CardTitle>
        <CardDescription className={CARD_DESC}>
          Configure dashboard telemetry cadence to optimize single-core CPU usage
          and device temperature.
        </CardDescription>
        <CardAction>
          <Badge variant="success" className="gap-1.5 font-mono text-xs">
            <RadioIcon className="size-3 flex-none" aria-hidden="true" />
            <span>SSE stream active · {activeOption.intervalSec}</span>
          </Badge>
        </CardAction>
      </CardHeader>

      <CardContent className={cn(CARD_PAD, CARD_BODY)}>
        <motion.div
          className={cn(ROW_GROUP, GROUP_FILL, "gap-2 p-2")}
          variants={staggerRows}
        >
          {POLLING_OPTIONS.map((option) => {
            const isSelected = option.mode === mode;
            const Icon = option.icon;

            return (
              <motion.div
                key={option.mode}
                variants={staggerRowItem}
                className="w-full"
              >
                <label
                  className={cn(
                    "group relative flex w-full items-start gap-3.5 rounded-field p-4 text-left transition-[background-color,color] duration-[var(--duration-standard)] ease-[var(--ease-standard)]",
                    "has-focus-visible:outline-none has-focus-visible:ring-[3px] has-focus-visible:ring-ring has-focus-visible:ring-offset-2 has-focus-visible:ring-offset-background",
                    "cursor-pointer select-none",
                    isSelected
                      ? "bg-primary-container text-on-primary-container shadow-xs"
                      : "bg-surface-container-high/70 text-on-surface hover:bg-surface-container-high hover:text-on-surface"
                  )}
                >
                  <input
                    type="radio"
                    name="polling-cadence"
                    value={option.mode}
                    checked={isSelected}
                    onChange={() => handleSelect(option.mode)}
                    className="sr-only"
                  />
                  <div
                    className={cn(
                      "flex size-10 flex-none items-center justify-center rounded-field transition-[background-color,color] duration-[var(--duration-standard)] ease-[var(--ease-standard)]",
                      isSelected
                        ? "bg-primary text-primary-foreground shadow-xs"
                        : "bg-surface-container-highest text-on-surface-variant group-hover:text-on-surface"
                    )}
                  >
                    <Icon className="size-5 flex-none" aria-hidden="true" />
                  </div>

                  <div className="flex min-w-0 flex-1 flex-col gap-1">
                    <div className="flex items-center justify-between gap-2">
                      <span
                        className={cn(
                          "text-[0.9375rem] font-semibold tracking-[-0.01em]",
                          isSelected
                            ? "text-on-primary-container"
                            : "text-on-surface"
                        )}
                      >
                        {option.title}
                      </span>
                      <div
                        className={cn(
                          "mt-0.5 grid size-5 flex-none place-items-center rounded-full border-2 transition-all duration-[var(--duration-quick)] ease-out",
                          isSelected
                            ? "border-primary bg-primary text-primary-foreground"
                            : "border-outline/50 text-transparent group-hover:border-outline"
                        )}
                        aria-hidden="true"
                      >
                        {isSelected ? (
                          <motion.span
                            className="size-2 rounded-full bg-primary-foreground"
                            initial={{ scale: 0 }}
                            animate={{ scale: 1 }}
                            transition={{ duration: 0.15, ease: "easeOut" }}
                          />
                        ) : null}
                      </div>
                    </div>

                    <p
                      className={cn(
                        "text-[0.8125rem] leading-relaxed text-pretty",
                        isSelected
                          ? "text-on-primary-container/85"
                          : "text-on-surface-variant"
                      )}
                    >
                      {option.subtext}
                    </p>
                  </div>
                </label>
              </motion.div>
            );
          })}
        </motion.div>
      </CardContent>
    </Card>
  );
}
