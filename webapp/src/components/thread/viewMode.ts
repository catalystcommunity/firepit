// Tree vs. flat "mailing-list order" toggle persistence (task C3 scope
// item 1: "a flat 'mailing-list order' toggle (persisted in localStorage)").
// One global preference (not per-thread) — a reader who prefers flat
// mailing-list order almost always prefers it everywhere.
export type ThreadViewMode = "tree" | "flat";

const KEY = "firepit:thread-view-mode";

export function readViewMode(): ThreadViewMode {
  try {
    const raw = typeof localStorage !== "undefined" ? localStorage.getItem(KEY) : null;
    return raw === "flat" ? "flat" : "tree";
  } catch {
    return "tree";
  }
}

export function writeViewMode(mode: ThreadViewMode): void {
  try {
    if (typeof localStorage !== "undefined") localStorage.setItem(KEY, mode);
  } catch {
    // localStorage unavailable (private browsing quota, SSR, etc.) — the
    // toggle still works for the session, it just won't persist.
  }
}
