// Store one view preference for all threads.
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
