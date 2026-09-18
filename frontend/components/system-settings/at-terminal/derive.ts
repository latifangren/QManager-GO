// =============================================================================
// AT Terminal — shared derivations
// =============================================================================
// `shapes.ts` owns how things look; this owns the console's state contract and
// how a stored entry becomes a string. Sibling of the same split in
// `components/system-settings/derive.ts`.
//
// The safety rules live here as PATTERN + KEY pairs rather than pattern +
// sentence: the sentence is a locale leaf, and a rule table holding English
// prose is a rule table that can only ever speak English.
// =============================================================================

import {
  AT_COMMAND_CATEGORIES,
  DEFAULT_AT_COMMANDS,
  type ATCommandCategory,
  type ATCommandDefault,
} from "@/constants/at-commands";

// -----------------------------------------------------------------------------
// The transcript
// -----------------------------------------------------------------------------

/** What became of one submitted command. */
export type EntryStatus = "success" | "error" | "blocked";

export interface HistoryEntry {
  id: string;
  command: string;
  response: string;
  status: EntryStatus;
  timestamp: number;
}

/** A command held behind the confirmation gate, with the rule that caught it. */
export interface PendingGate {
  command: string;
  rule: WarningKey;
}

export const STORAGE_KEY = "qm_at_history";
export const MAX_HISTORY = 100;
export const CGI_ENDPOINT = "/cgi-bin/quecmanager/at_cmd/send_command.sh";

export function generateId(): string {
  try {
    return crypto.randomUUID();
  } catch {
    return Date.now().toString(36) + Math.random().toString(36).slice(2, 8);
  }
}

/**
 * Splits AT command arguments by comma while respecting double quotes.
 */
function splitAtArgs(argsStr: string): string[] {
  const args: string[] = [];
  let current = "";
  let inQuotes = false;
  for (let i = 0; i < argsStr.length; i++) {
    const char = argsStr[i];
    if (char === '"') {
      inQuotes = !inQuotes;
      current += char;
    } else if (char === "," && !inQuotes) {
      args.push(current.trim());
      current = "";
    } else {
      current += char;
    }
  }
  args.push(current.trim());
  return args;
}

/** Masks a single argument, preserving double quotes if present. */
function maskArg(arg: string): string {
  if (!arg) return arg;
  if (arg.startsWith('"') && arg.endsWith('"')) {
    return '"****"';
  }
  return "****";
}

/**
 * Sanitizes/redacts sensitive arguments (PINs, PUKs, passwords) in AT commands
 * or command echoes so secrets are not saved in plaintext in browser localStorage.
 */
export function redactSensitiveCommand(text: string): string {
  if (!text) return text;
  let result = text;

  // 1. CPIN / CPIN2 / QPIN / EPIN: all arguments are PINs/PUKs. Avoid matching test query AT+CPIN=?
  result = result.replace(
    /(^|[;\s])((?:AT)?\+(?:[CQE])?PIN2?\s*=\s*)(?!\?)([^;\r\n]+)/gi,
    (m, p1, p2, args) => {
      const parts = splitAtArgs(args);
      return p1 + p2 + parts.map(maskArg).join(",");
    },
  );

  // 2. CPWD: AT+CPWD=<fac>,<oldpwd>,<newpwd>
  result = result.replace(
    /(^|[;\s])((?:AT)?\+CPWD\s*=\s*)(?!\?)([^;\r\n]+)/gi,
    (m, p1, p2, args) => {
      const parts = splitAtArgs(args);
      if (parts.length >= 2) parts[1] = maskArg(parts[1]);
      if (parts.length >= 3) parts[2] = maskArg(parts[2]);
      return p1 + p2 + parts.join(",");
    },
  );

  // 3. CLCK: AT+CLCK=<fac>,<mode>[,<passwd>]
  result = result.replace(
    /(^|[;\s])((?:AT)?\+CLCK\s*=\s*)(?!\?)([^;\r\n]+)/gi,
    (m, p1, p2, args) => {
      const parts = splitAtArgs(args);
      if (parts.length >= 3) parts[2] = maskArg(parts[2]);
      return p1 + p2 + parts.join(",");
    },
  );

  // 4. QCPDPP: AT$QCPDPP=<pdp_idx>,<auth_type>[,<password>[,<username>]]
  result = result.replace(
    /(^|[;\s])((?:AT)?\$QCPDPP\s*=\s*)(?!\?)([^;\r\n]+)/gi,
    (m, p1, p2, args) => {
      const parts = splitAtArgs(args);
      if (parts.length >= 3) parts[2] = maskArg(parts[2]);
      return p1 + p2 + parts.join(",");
    },
  );

  // 5. QICSGP: AT+QICSGP=<cid>,<context_type>[,<apn>[,<username>[,<password>[,<auth_type>]]]]
  result = result.replace(
    /(^|[;\s])((?:AT)?\+QICSGP\s*=\s*)(?!\?)([^;\r\n]+)/gi,
    (m, p1, p2, args) => {
      const parts = splitAtArgs(args);
      if (parts.length >= 5) parts[4] = maskArg(parts[4]);
      return p1 + p2 + parts.join(",");
    },
  );

  // 6. CGAUTH: AT+CGAUTH=<cid>,<auth_prot>[,<password>[,<username>]]
  result = result.replace(
    /(^|[;\s])((?:AT)?\+CGAUTH\s*=\s*)(?!\?)([^;\r\n]+)/gi,
    (m, p1, p2, args) => {
      const parts = splitAtArgs(args);
      if (parts.length >= 3) parts[2] = maskArg(parts[2]);
      return p1 + p2 + parts.join(",");
    },
  );

  // 7. Generic password keywords: password="...", passwd="...", pwd="..."
  result = result.replace(/\b(password|passwd|pwd)\s*=\s*"[^"]*"/gi, '$1="****"');
  result = result.replace(/\b(password|passwd|pwd)\s*=\s*([^,\s"';\r\n]+)/gi, "$1=****");

  return result;
}

/** Sanitize an entry's command and response before storing. */
export function sanitizeHistoryEntry(entry: HistoryEntry): HistoryEntry {
  return {
    ...entry,
    command: redactSensitiveCommand(entry.command),
    response: redactSensitiveCommand(entry.response),
  };
}

export function loadHistory(): HistoryEntry[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed) ? parsed.map(sanitizeHistoryEntry) : [];
  } catch {
    return [];
  }
}

export function saveHistory(entries: HistoryEntry[]): void {
  const sanitized = entries.map(sanitizeHistoryEntry);
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(sanitized));
  } catch {
    // Quota exceeded — trim to half and retry.
    try {
      const trimmed = sanitized.slice(-Math.floor(MAX_HISTORY / 2));
      localStorage.setItem(STORAGE_KEY, JSON.stringify(trimmed));
    } catch {
      // Still failing — degrade to in-memory only.
    }
  }
}

const pad2 = (n: number): string => String(n).padStart(2, "0");

/**
 * A wall-clock stamp in the SAME zone the transcript column reads in, with the
 * offset spelled out — `toISOString()` here silently shifted the file to UTC.
 */
export function exportStamp(atMs: number): string {
  const d = new Date(atMs);
  const offset = -d.getTimezoneOffset();
  const sign = offset < 0 ? "-" : "+";
  const abs = Math.abs(offset);
  const date = `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}`;
  const time = `${pad2(d.getHours())}:${pad2(d.getMinutes())}:${pad2(d.getSeconds())}`;
  return `${date} ${time} ${sign}${pad2(Math.floor(abs / 60))}:${pad2(abs % 60)}`;
}

export function formatExport(entries: HistoryEntry[]): string {
  return entries
    .map((e) => `[${exportStamp(e.timestamp)}] ❯ ${e.command}\n${e.response}`)
    .join("\n\n");
}

/** One entry's transcript column: the wall clock it was stamped with. */
export function clockTime(atMs: number): string {
  return new Date(atMs).toLocaleTimeString(undefined, {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  });
}

/** The whole entry, as one clipboard string. */
export function entryToText(entry: HistoryEntry): string {
  return `${entry.command}\n${entry.response}`;
}

// -----------------------------------------------------------------------------
// Safety rules
// -----------------------------------------------------------------------------

const BLOCKED_RULES = [
  { key: "qscanfreq", pattern: /\bQSCANFREQ\b/i },
  { key: "qscan", pattern: /\bQSCAN\b/i },
  { key: "resetfactory", pattern: /QCFG\s*=\s*"resetfactory"/i },
] as const;

// `reboot` comes FIRST: `AT+CFUN=1,1` is the harder hazard, and a `[04]` class
// can never match it, so ordering is what keeps the two rules from swapping.
const WARNING_RULES = [
  { key: "reboot", pattern: /CFUN\s*=\s*1\s*,\s*1\b/i },
  { key: "radio_off", pattern: /CFUN\s*=\s*[04]\b/i },
] as const;

export type BlockedKey = (typeof BLOCKED_RULES)[number]["key"];
export type WarningKey = (typeof WARNING_RULES)[number]["key"];

/** The rule that refuses this command outright, or null. */
export function matchBlocked(command: string): BlockedKey | null {
  return BLOCKED_RULES.find((r) => r.pattern.test(command))?.key ?? null;
}

/** The rule that puts this command behind the confirmation gate, or null. */
export function matchWarning(command: string): WarningKey | null {
  return WARNING_RULES.find((r) => r.pattern.test(command))?.key ?? null;
}

export const GAME_COMMAND = "AT+GAME";

// -----------------------------------------------------------------------------
// Following the foot of the transcript
// -----------------------------------------------------------------------------

/** How far off the foot still counts as "reading the newest line". */
export const FOLLOW_SLACK_PX = 56;

/**
 * Whether the reader is at the foot. An unconditional scroll yanks the view out
 * from under someone who has scrolled up to read an earlier response, which is
 * the one thing a transcript must never do.
 */
export function isNearBottom(el: HTMLElement | null): boolean {
  if (!el) return false;
  return el.scrollHeight - el.scrollTop - el.clientHeight <= FOLLOW_SLACK_PX;
}

export function prefersReducedMotion(): boolean {
  return (
    typeof window !== "undefined" &&
    window.matchMedia("(prefers-reduced-motion: reduce)").matches
  );
}

// -----------------------------------------------------------------------------
// The commands popover
// -----------------------------------------------------------------------------

export interface CommandGroupSpec {
  category: ATCommandCategory;
  items: ATCommandDefault[];
}

/**
 * The built-ins, split by function. Empty categories are dropped, so adding a
 * category with no members never renders an empty heading.
 */
export function groupedDefaults(): CommandGroupSpec[] {
  return AT_COMMAND_CATEGORIES.map((category) => ({
    category,
    items: DEFAULT_AT_COMMANDS.filter((preset) => preset.category === category),
  })).filter((group) => group.items.length > 0);
}
