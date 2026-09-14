"use client";

import { Skeleton } from "@/components/ui/skeleton";
import { CARD_SHELL } from "./shapes";

const CHART_H = "h-[250px]";

export function SignalHistorySkeleton() {
  return (
    <div className={CARD_SHELL}>
      <div className="px-0 flex flex-col space-y-1.5 p-6">
        <Skeleton className="h-6 w-36" />
        <Skeleton className="h-4 w-64" />
      </div>
      <div className="px-0 p-6 pt-0">
        <Skeleton className={`w-full rounded-field ${CHART_H}`} />
      </div>
      <div className="flex flex-col gap-1 px-0 p-6 pt-0">
        <Skeleton className="h-4 w-48" />
        <Skeleton className="h-3 w-72" />
      </div>
    </div>
  );
}
