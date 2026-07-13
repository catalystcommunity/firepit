// Theme mechanics: a stored user choice wins; otherwise Firepit defaults to
// light so the forum reads as an approachable project workspace on first
// load. `index.html`'s inline paint-guard script (a plain JS copy of this
// same rule) sets `data-theme` on `<html>` before first paint; this module
// is what the in-app toggle (AppShell's `ThemeToggle`) reads/writes
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
  if (typeof document === "undefined") return "light";
  return document.documentElement.getAttribute("data-theme") === "dark" ? "dark" : "light";
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
