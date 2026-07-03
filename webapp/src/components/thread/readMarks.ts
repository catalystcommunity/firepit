// Client-side read-mark bookkeeping for the thread view (task C3,
// PLANDOC.md §4 "Read model is hybrid"). ReadService's watermark
// (`read_marks.last_read_at`) is per-*post*, and CSIL has no op that hands
// the raw timestamp back to the client (`unread-summary` only aggregates to
// counts) — so there is no server-provided value to diff comment timestamps
// against for "highlight what's new since I last looked". This module is a
// deliberate client-only stand-in for that missing read: a per-post
// "last viewed" timestamp cached in `localStorage`, used purely to decide
// which comments to highlight as unread; it never substitutes for the real
// watermark write (`api.read.markRead`/`markUnread` are still the source of
// truth the server acts on).
//
// Per-item overrides (the `unread_overrides` table, PLANDOC.md §4: "explicit
// 'keep this unread' pins, beats the watermark") are mirrored the same way:
// a small local set, keyed `${targetType}:${targetId}`, toggled by the
// per-item mark read/unread affordance so the UI reflects the pin
// immediately without waiting on a poll. The mock's `FixtureStore` only
// tracks read state at post granularity today (see store.ts's
// `readPostIds`), so a mock-mode "mark this comment unread" call collapses
// to "mark the whole post unread" server-side — a known mock limitation,
// not something this module can paper over; the local override still gives
// the correct per-item *display* regardless.
const LAST_VIEWED_PREFIX = "firepit:thread:last-viewed:";
const OVERRIDES_KEY = "firepit:thread:unread-overrides";

function hasStorage(): boolean {
  try {
    return typeof localStorage !== "undefined";
  } catch {
    return false;
  }
}

export function readLastViewed(postId: string): Date | null {
  if (!hasStorage()) return null;
  const raw = localStorage.getItem(LAST_VIEWED_PREFIX + postId);
  if (!raw) return null;
  const ms = Number(raw);
  return Number.isFinite(ms) ? new Date(ms) : null;
}

export function writeLastViewed(postId: string, at: Date): void {
  if (!hasStorage()) return;
  localStorage.setItem(LAST_VIEWED_PREFIX + postId, String(at.getTime()));
}

type OverrideKey = string;

function overrideKey(targetType: "post" | "comment", targetId: string): OverrideKey {
  return `${targetType}:${targetId}`;
}

function readOverrides(): Set<OverrideKey> {
  if (!hasStorage()) return new Set();
  try {
    const raw = localStorage.getItem(OVERRIDES_KEY);
    return new Set(raw ? (JSON.parse(raw) as OverrideKey[]) : []);
  } catch {
    return new Set();
  }
}

function writeOverrides(overrides: Set<OverrideKey>): void {
  if (!hasStorage()) return;
  localStorage.setItem(OVERRIDES_KEY, JSON.stringify([...overrides]));
}

export function isUnreadOverride(targetType: "post" | "comment", targetId: string): boolean {
  return readOverrides().has(overrideKey(targetType, targetId));
}

export function setUnreadOverride(targetType: "post" | "comment", targetId: string, unread: boolean): void {
  const overrides = readOverrides();
  const key = overrideKey(targetType, targetId);
  if (unread) overrides.add(key);
  else overrides.delete(key);
  writeOverrides(overrides);
}
