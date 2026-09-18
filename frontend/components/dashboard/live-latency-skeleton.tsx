"use client";

import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { CARD_SHELL, ROW } from "./shapes";

const CHART_BOX = "min-h-[150px] flex-1";

export function LiveLatencySkeleton() {
  return (
    <div className={cn(CARD_SHELL, "flex h-full flex-col")}>
      <div className="flex h-full flex-col gap-3.5">
        <div className="flex items-center gap-3">
          <Skeleton className="h-6 w-40" />
          <Skeleton className="ml-auto h-7 w-24 rounded-pill" />
        </div>
        <div className="flex flex-col gap-1.5">
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-2/3" />
        </div>
        <Skeleton className={cn("w-full rounded-field", CHART_BOX)} />
        <div className="flex items-center gap-4">
          <Skeleton className="h-3 w-20" />
          <Skeleton className="h-3 w-24" />
        </div>
        <Skeleton className={cn("mt-auto w-full rounded-pill", ROW.HEIGHT)} />
      </div>
    </div>
  );
}
