export type Tone = "ok" | "partial" | "bad" | "warn" | "muted";

export const STATES: Record<number, { short: string; long: string; tone: Tone }> = {
  1: { short: "AC", long: "Accepted", tone: "ok" },
  2: { short: "PC", long: "Partially Correct", tone: "partial" },
  3: { short: "WA", long: "Wrong Answer", tone: "bad" },
  4: { short: "RE", long: "Runtime Error", tone: "bad" },
  5: { short: "RE(SIG)", long: "Runtime Error (signal)", tone: "bad" },
  6: { short: "TLE", long: "Time Limit Exceeded", tone: "warn" },
  7: { short: "MLE", long: "Memory Limit Exceeded", tone: "warn" },
  8: { short: "OLE", long: "Output Limit Exceeded", tone: "warn" },
  9: { short: "CE", long: "Compile Error", tone: "warn" },
  10: { short: "CLE", long: "Compilation Limit Exceeded", tone: "warn" },
  11: { short: "IE", long: "Internal Error", tone: "muted" },
  12: { short: "JE", long: "Judge Error", tone: "muted" },
  100: { short: "Judging", long: "Judging", tone: "muted" },
  101: { short: "Pending", long: "Pending", tone: "muted" },
  102: { short: "SK", long: "Skipped", tone: "muted" },
  103: { short: "RJ", long: "Rejected", tone: "muted" },
};

export const isFinal = (state: number) => state < 100 || state > 101;

const dateTime = new Intl.DateTimeFormat(undefined, {
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hour12: false,
});

export function fmtTime(value: string | Date | null | undefined): string {
  if (!value) return "—";
  const d = new Date(value);
  return Number.isNaN(d.getTime()) ? "—" : dateTime.format(d);
}

export function fmtDuration(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds));
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  const pad = (n: number) => String(n).padStart(2, "0");
  return (d ? `${d}d ` : "") + `${pad(h)}:${pad(m)}:${pad(s % 60)}`;
}

export function fmtMemory(kb: number): string {
  return kb >= 1024 ? `${(kb / 1024).toFixed(1)} MiB` : `${kb} KiB`;
}

export function fmtSize(bytes: number): string {
  if (bytes >= 1 << 20) return `${(bytes / (1 << 20)).toFixed(1)} MiB`;
  if (bytes >= 1024) return `${(bytes / 1024).toFixed(1)} KiB`;
  return `${bytes} B`;
}

/** Value for <input type="datetime-local"> in the browser's time zone. */
export function toLocalInput(value: string | Date): string {
  const d = new Date(value);
  const local = new Date(d.getTime() - d.getTimezoneOffset() * 60000);
  return local.toISOString().slice(0, 16);
}

export const fromLocalInput = (value: string) => new Date(value).toISOString();

/** Parses "1, 2 5-7" into [1, 2, 5, 6, 7]. Returns null when malformed. */
export function parseIdList(text: string): number[] | null {
  const out: number[] = [];
  for (const part of text.split(/[\s,]+/).filter(Boolean)) {
    const m = /^(\d+)(?:-(\d+))?$/.exec(part);
    if (!m) return null;
    const from = Number(m[1]);
    const to = m[2] ? Number(m[2]) : from;
    if (to < from || to - from > 2000) return null;
    for (let i = from; i <= to; i++) out.push(i);
  }
  return out;
}
