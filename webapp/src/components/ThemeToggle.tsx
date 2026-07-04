// The topbar's sun/moon theme toggle (docs/DESIGN.md "theme mechanics").
// A plain button — keyboard-operable for free, no custom key handling
// needed — that flips `data-theme` on `<html>` via `~/lib/theme` and
// re-renders its own glyph/label from the new state. `index.html`'s inline
// paint-guard script already set the initial `data-theme` before this ever
// mounts; `getCurrentTheme()` just reads what it landed on.
import { createSignal, type Component } from "solid-js";
import { getCurrentTheme, setTheme, type Theme } from "~/lib/theme";

const ThemeToggle: Component = () => {
  const [theme, setThemeSignal] = createSignal<Theme>(getCurrentTheme());

  const toggle = (): void => {
    const next: Theme = theme() === "dark" ? "light" : "dark";
    setTheme(next);
    setThemeSignal(next);
  };

  return (
    <button
      type="button"
      class="theme-toggle"
      onClick={toggle}
      aria-label={theme() === "dark" ? "Switch to light theme" : "Switch to dark theme"}
      title={theme() === "dark" ? "Switch to light theme" : "Switch to dark theme"}
    >
      <span aria-hidden="true">{theme() === "dark" ? "🌙" : "☀️"}</span>
    </button>
  );
};

export default ThemeToggle;
