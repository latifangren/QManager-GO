"use client";

import { useState } from "react";
import { ChevronDownIcon, ChevronRightIcon } from "lucide-react";
import HealthStatusBadge from "./health-status-badge";
import type { HealthCheckTest } from "@/types/system-health-check";
import { cn } from "@/lib/utils";

interface TestRowProps {
  test: HealthCheckTest;
  fetchOutput: (testId: string) => Promise<string>;
}

export default function TestRow({ test, fetchOutput }: TestRowProps) {
  const [expanded, setExpanded] = useState(false);
  const [output, setOutput] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const expandable = test.status === "fail" || test.status === "warn";

  const toggle = async () => {
    if (!expandable) return;
    const next = !expanded;
    setExpanded(next);
    if (next && output === null) {
      setLoading(true);
      setError(null);
      try {
        const body = await fetchOutput(test.id);
        setOutput(body || "(no output captured)");
      } catch (e) {
        setError(e instanceof Error ? e.message : String(e));
      } finally {
        setLoading(false);
      }
    }
  };

  return (
    <div className="border-b last:border-b-0">
      <button
        type="button"
        onClick={toggle}
        disabled={!expandable}
        className={cn(
          "flex w-full items-center justify-between gap-3 py-2 px-1 text-left",
          expandable ? "cursor-pointer hover:bg-muted/40" : "cursor-default",
        )}
      >
        <div className="flex items-center gap-2 min-w-0">
          {expandable ? (
            expanded
              ? <ChevronDownIcon className="size-4 text-muted-foreground shrink-0" />
              : <ChevronRightIcon className="size-4 text-muted-foreground shrink-0" />
          ) : (
            <span className="size-4 shrink-0" aria-hidden />
          )}
          <div className="min-w-0">
            <div className="text-sm font-medium truncate">{test.label}</div>
            {test.detail && (
              <div className="text-xs text-muted-foreground truncate">{test.detail}</div>
            )}
          </div>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          {test.status !== "pending" && test.status !== "running" && test.duration_ms > 0 && (
            <span className="text-xs text-muted-foreground tabular-nums">{test.duration_ms}ms</span>
          )}
          <HealthStatusBadge status={test.status} />
        </div>
      </button>
      {expanded && (
        <div className="bg-muted/40 border-t px-3 py-2">
          {loading && <div className="text-xs text-muted-foreground">Loading…</div>}
          {error && <div className="text-xs text-destructive">Failed to load output: {error}</div>}
          {output !== null && (
            <pre className="text-xs whitespace-pre-wrap break-words font-mono max-h-64 overflow-auto">{output}</pre>
          )}
        </div>
      )}
    </div>
  );
}
