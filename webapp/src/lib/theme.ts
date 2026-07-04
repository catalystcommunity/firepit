// Blue Flame theme mechanics (docs/DESIGN.md): dark is the default look
// regardless of the OS `prefers-color-scheme` — a forum should look the
// same for everyone unless they say otherwise. `index.html`'s inline
// paint-guard script (a plain JS copy of this same "stored choice wins,
// else dark" rule — it must run before any module loads, so it can't
// import this file) sets `data-theme` on `<html>` before first paint; this
// module is what the in-app toggle (AppShell's `ThemeToggle`) reads/writes
// afterwards, so the two stay in lockstep.
const STORAGE_KEY = "firepit-theme";

export type Theme = "dark" | "light";

function isTheme(value: string | null): value is Theme {
  return value === "dark" || value === "light";
}

/** The theme the user has explicitly chosen, if any — `null` means "no stored choice yet". */
export function getStoredTheme(): Theme | null {
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    return isTheme(stored) ? stored : null;
  } catch {
    // Storage can throw in locked-down/private-browsing contexts; a missing
    // stored choice just means "use the default" either way.
    return null;
  }
}

/** Whatever `data-theme` is currently on `<html>` (set by the paint-guard script on load,
 * or by `setTheme` afterwards) — the single source of truth for "what's showing right now". */
export function getCurrentTheme(): Theme {
  if (typeof document === "undefined") return "dark";
  return document.documentElement.getAttribute("data-theme") === "light" ? "light" : "dark";
}

/** Applies `theme` to `<html>` and persists it as the caller's explicit choice. */
export function setTheme(theme: Theme): void {
  document.documentElement.setAttribute("data-theme", theme);
  try {
    localStorage.setItem(STORAGE_KEY, theme);
  } catch {
    // Best-effort persistence — the theme still applies for this page view.
  }
}
