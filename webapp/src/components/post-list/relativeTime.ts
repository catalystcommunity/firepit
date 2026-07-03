// A short, mailing-list-quiet relative timestamp for post rows (task C2).
// Tabular info like this stays secondary/quiet per the design direction —
// plain text, no color, not the visual focus of a row.
const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

/**
 * "2h ago", "3d ago", etc. Falls back to an absolute `YYYY-MM-DD` past ~30
 * days, since "42d ago" stops being useful at a glance. `now` is injectable
 * for deterministic tests.
 */
export function relativeTime(date: Date, now: Date = new Date()): string {
  const diff = now.getTime() - date.getTime();
  if (diff < MINUTE) return "just now";
  if (diff < HOUR) return `${Math.floor(diff / MINUTE)}m ago`;
  if (diff < DAY) return `${Math.floor(diff / HOUR)}h ago`;
  if (diff < 30 * DAY) return `${Math.floor(diff / DAY)}d ago`;
  return date.toISOString().slice(0, 10);
}
