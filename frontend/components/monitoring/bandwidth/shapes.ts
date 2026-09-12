// =============================================================================
// Bandwidth & vnStat Monitoring — Geometry and Tone Contract
// =============================================================================

export const PAGE_ROOT =
  "@container/main mx-auto flex flex-col gap-6 px-3 pb-8 lg:px-6";

export const PAGE_HEAD = {
  ROOT: "flex flex-col gap-4 @3xl/main:flex-row @3xl/main:items-center @3xl/main:justify-between",
  TITLES: "flex flex-col gap-1.5",
  TITLE: "text-2xl lg:text-3xl font-bold tracking-tight text-on-surface",
  DESC: "text-xs lg:text-sm text-on-surface-variant leading-relaxed text-pretty",
  ACTIONS: "flex flex-wrap items-center gap-2.5",
} as const;

export const PILL_ACTION =
  "h-9 gap-2 rounded-pill px-4 text-xs lg:text-sm font-medium pointer-coarse:h-11";

export const CARD_SHELL =
  "@container/card rounded-card border-0 bg-surface py-5 px-5 lg:px-6 shadow-[var(--shadow-whisper)]!";

export const CARD_PAD = "p-0";

export const CARD_TITLE = "text-base lg:text-lg font-semibold leading-tight text-on-surface";

export const CARD_DESC = "text-xs lg:text-sm text-on-surface-variant leading-relaxed";

export const SUMMARY_GRID =
  "grid grid-cols-1 gap-3.5 sm:grid-cols-2 @4xl/main:grid-cols-4";

export const FOCUS_RING =
  "focus-visible:outline-none focus-visible:ring-[2px] focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background";
