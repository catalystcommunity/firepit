# Firepit visual design — "Blue Flame"

This is the design system the webapp (`webapp/src/index.css` + the
per-component CSS files under `webapp/src/components/**`) implements. It
exists so future UI work stays coherent instead of re-deriving "what color
is this" or "is this a card or a row" from scratch each time.

**Concept:** the hottest part of a flame is blue. Dark mode (the default)
is the night around a campfire — layered deep-navy surfaces, never pure
black. Light mode is daylight sky. Firepit is a mailing-list-ethos forum
(threaded, endorsements-by-name, no gamification) — the design reads as
calm and framed, a forum you want to read, not an engagement machine.

## 1. Tokens

All tokens are CSS custom properties on `:root`, overridden by
`:root[data-theme="light"]`. Dark values live directly on `:root` since
dark is the default (see §2).

| Token | Dark (default) | Light | Use |
|---|---|---|---|
| `--bg0` | `#0B1220` | `#F5F8FC` | Page background. |
| `--bg1` | `#111A2C` | `#FFFFFF` | Panel / card surface. |
| `--bg2` | `#16233A` | `#EDF3FB` | Raised surface: row hover, dropdowns, active nav item. |
| `--border` | `#233450` | `#D8E2F0` | Hairline panel/row borders. |
| `--border-strong` | `#33496B` | `#B9C9E0` | Chip borders, dropdown borders — anything that needs to read a step stronger than a hairline. |
| `--text` | `#E6EDF7` | `#17263D` | Body text. |
| `--text-muted` | `#8FA3C0` | `#5B6E8C` | Secondary text and — paired with mono, see §3 — all metadata. |
| `--accent` | `#4D9FFF` | `#1D6BDA`* | Blue-flame core: links, button/toggle accents, "is-self"/"subscribed" states. |
| `--accent-hi` | `#77C4FF` | `#1557B8` | Cyan flame tip: link hover, gradient end stop, focus ring color. |
| `--ember` | `#FF9F5A` | `#C96A1E` | **Unread/still-burning only.** Never decorative — see §5. |
| `--danger` | `#F2708A` | `#C4314F` | Destructive actions/errors only. |

Derived tokens: `--radius` (10px, panels), `--radius-sm` (6px, controls/
chips), `--space-1/2/3` (8/16/24px — the panel-padding grid), `--shadow-panel`
(`none` in dark — dark mode separates surfaces by lightness, not shadow —
a soft double shadow in light mode), `--gradient-flame` (see §4),
`--transition-fast` (140ms ease).

**Contrast check** (computed via the standard relative-luminance formula,
not eyeballed): `--text` and `--text-muted` on `--bg0`/`--bg1`/`--bg2` all
clear 4.5:1 (WCAG AA, normal text) in both themes:

| Pair | Dark | Light |
|---|---|---|
| `--text` on `--bg0` | 15.9:1 | 14.3:1 |
| `--text` on `--bg1` | 14.8:1 | 15.2:1 |
| `--text-muted` on `--bg0` | 7.3:1 | 4.9:1 |
| `--text-muted` on `--bg1` | 6.8:1 | 5.2:1 |
| `--text-muted` on `--bg2` | 6.1:1 | 4.6:1 |

*\*One adjustment beyond `--text-muted`:* `--accent` also renders as text
(it's the link color, not just a decorative accent), and the spec's light
value `#1F6FE0` measured **4.47:1** on `--bg0` — just under the 4.5:1 floor.
Nudged minimally darker to `#1D6BDA` (4.73:1 on `--bg0`, 5.04:1 on `--bg1`)
— same hue, indistinguishable from the spec value at a glance. Dark
`--accent` (`#4D9FFF`) and both themes' `--accent-hi` were already well
clear (6.4–9.9:1) and weren't touched.

## 2. Theme mechanics

- `data-theme="dark"|"light"` lives on `<html>`. **Dark is the default
  regardless of `prefers-color-scheme`** — a stored choice
  (`localStorage["firepit-theme"]`) always wins if present; otherwise dark.
- `index.html` has a small inline `<script>` in `<head>` that sets
  `data-theme` before first paint (a plain-JS copy of the same "stored
  choice wins, else dark" rule, since it must run before any module —
  including `~/lib/theme.ts` — loads). This is the paint guard: there is no
  flash of the wrong theme.
- `~/lib/theme.ts` is the runtime source of truth after that: `getStoredTheme`,
  `getCurrentTheme`, `setTheme`. `~/components/ThemeToggle.tsx` is the
  topbar's sun/moon button — a plain, keyboard-operable `<button>` (no
  custom key handling needed) that flips the theme and re-renders its own
  glyph/label.
- Every `@media (prefers-color-scheme: dark)` query that used to exist in
  this codebase (`index.css`) has been migrated to the `[data-theme]` token
  system — none remain anywhere in `webapp/src`.

## 3. Type

- Body/UI font: IBM Plex Sans (400/500/600/700). Monospace: IBM Plex Mono
  (400/500). Both are `@fontsource` npm deps imported in `index.tsx` (one
  `.css` import per weight actually used) and bundled by Vite — **never** an
  external CDN/font URL, since production runs under a strict CSP.
- Post/comment bodies (`.md-body`): line-height 1.6, ~72ch max measure.
- **The structural device:** prose is Sans; data is Mono, everywhere,
  consistently. Every metadata scrap — timestamps, `@handles`, comment/board
  counts, board slugs, the GitHub-origin `gh` chip, revision stamps — renders
  in IBM Plex Mono at ~0.8em, colored `--text-muted`. This one rule (one
  shared selector list in `index.css`, not per-component overrides) is what
  makes "this is prose, this is data" legible at a glance across every
  screen. If you add a new piece of metadata anywhere in the app, add its
  selector to that list rather than hand-styling it.

## 4. Signature motif — the exactly-four-places rule

`--gradient-flame` (`linear-gradient(135deg, var(--accent), var(--accent-hi))`)
is Firepit's one visual flourish, and it is deliberately rationed. It (or,
for the focus ring, its end-stop color alone) appears in **exactly four
places** in the entire app:

1. **The flame mark** (`~/components/FlameMark.tsx`) — the small
   teardrop/flame SVG left of the "Firepit" wordmark in the topbar, filled
   with the gradient via SVG `<stop>` colors.
2. **The active nav item's left edge** — a 2px gradient bar on whichever
   board is current in the board rail (`.board-rail a.active::before`;
   `.active` comes for free from `@solidjs/router`'s `<A>`).
3. **The focus-visible ring** (`:focus-visible { box-shadow: ... var(--accent-hi) ... }`)
   — solid `--accent-hi`, not a two-stop gradient (a box-shadow can't
   render one), but it's the same "flame tip" color and the same rationing
   rule applies: this is the *only* place focus is indicated with color
   alone beyond the browser default outline being suppressed in its favor.
4. **Primary buttons** (`.btn-primary` and the other submit-style buttons
   listed alongside it in `index.css`'s "buttons" section) — gradient
   background, `#0B1220` text in dark mode (checked for contrast against
   the lightest gradient stop), white text in light mode.

Nowhere else in the app does a gradient appear. Secondary buttons are
`--bg2` + `--border`; ghost/cancel buttons are transparent + `--border`;
destructive actions get a `--danger` outline (or `--danger` text, for the
text-link-style destructive actions in thread content-actions). If a future
change wants to add gradient somewhere else, that's a deliberate
spec change to this document, not a one-off.

## 5. Ember means unread — nowhere else

`--ember` renders in exactly these places, all of them "there is unread
content here": the board-rail unread dot, board-index/post-list unread
dots, the notification bell's count pill, and the unread notification row's
2px left edge. It is **never** used decoratively, for emphasis, or for any
other kind of status — that's what `--accent` (active/links) and `--danger`
(errors/destructive) are for. If it shows up somewhere that isn't "you
haven't read this yet," that's a bug against this spec.

## 6. Framing / separation

- **Panels**: `--bg1` surface, 1px `--border`, `--radius` (10px), padding on
  the 8px grid (`--space-2`/`--space-3` = 16/24px). Page background is
  `--bg0`, so panels visibly float above it. `--shadow-panel` adds a subtle
  lift in light mode only — dark mode separates purely by lightness.
- **App shell**: topbar is `--bg1` + a bottom hairline; the board rail is a
  bordered panel column; the content column (`.page`) caps at ~880px,
  centered, with breathing room around it.
- **Board index / post lists**: each board/post list is *one shared panel*
  (not a stack of individually-floating cards) — rows inside separate by
  hairline, hover = `--bg2`. This is a deliberate "reads as a list" choice
  over "reads as a card grid": mailing-list ethos, not a social feed.
- **Thread view**: the root post is the one framed panel in the whole
  thread. Comments below it are *not* individually boxed — they keep their
  indent rails (`--border`, turning `--accent` on hover of that subtree)
  and separate by hairline + spacing only. Tombstones render at 60%
  opacity. GitHub-origin content keeps its glyph, rendered Mono + muted
  inside a small bordered chip.
- **Composers/forms**: inputs sit on `--bg0` *inside* the `--bg1` panel (the
  panel is the raised surface; the field is the inset one). Primary button =
  gradient; secondary = `--bg2` + border; destructive = `--danger` outline.
- **Endorsements**: endorser names render inline in Mono, muted; role badges
  are tiny bordered chips (`--border-strong`, no fill) — visible, not
  gamified.
- **Empty states**: a friendly one-liner plus the action to take (e.g. "No
  posts yet — be the first."), never sad-face filler.

## 7. Motion

Transitions are 140ms ease, and only on `background-color`/`border-color`/
`color`/`box-shadow` — never layout, never an entrance animation.
`@media (prefers-reduced-motion: reduce)` disables all transitions and
animations app-wide (including the permalink-highlight flash).

## 8. Mobile patterns (≲760px)

Mobile-friendliness is a first-class part of this design, not just a
"doesn't break" floor.

- **Navigation**: below ~760px the board rail stops being a permanent
  column (there's no room for it next to an 880px content measure) and
  becomes an off-canvas drawer — `.board-rail` switches to
  `position: fixed`, slid off-screen via `transform: translateX(-100%)`,
  and slid in via `.is-open`. It's opened by a hamburger button
  (`.rail-toggle`, topbar-left, hidden above the breakpoint) and closed by
  a backdrop click, its own close button (`.rail-close`), pressing Escape,
  or simply navigating (AppShell closes it on every route change). All of
  that lives in `~/components/AppShell.tsx`; the rail's own CSS is what
  actually differs by breakpoint, not a second copy of the markup.
- **Topbar at 360px**: `flex-wrap: nowrap` plus `min-width: 0` on the
  shrinking pieces — the brand text and the logged-in display name
  (`.auth-name`) both truncate with an ellipsis before anything wraps or
  overflows. The theme toggle and notification bell are icon-only already
  (no label to drop), so they're unaffected by width.
- **Touch targets**: `@media (pointer: coarse)` raises every interactive
  control to a ≥44px hit target (WCAG 2.5.5) — via `min-height`/`min-width`
  for standalone icon buttons (theme toggle, bell, rail toggle/close), and
  via padding or an expanded absolutely-positioned hit area for controls
  embedded in tighter layouts. `.collapse-toggle` (the comment-tree
  expand/collapse button) is the one that needs the latter trick: it sits
  inside the per-depth comment-rail, so growing its *visual* box would
  widen every nesting level's indent along with it. Instead it stays
  visually small and gets a `::after` pseudo-element with `inset: -15px`
  to expand only the clickable area.
- **Thread nesting on narrow screens**: the fixture's deepest thread is 6
  levels (`webapp/src/lib/mock/fixtures.ts`'s welcome-post chain). Each
  nesting level's total indent is `.comment-rail`'s width plus
  `.comment-node`'s flex `gap` plus `.comment-children`'s margin+padding,
  and it compounds with depth — at the desktop sizing (1.1rem rail + gap +
  1.45rem margin/padding) that's fine, but unshrunk it would eat well over
  half of a narrow phone viewport by depth 6. Below 760px all three shrink
  together (rail down to 0.6rem, `.comment-node` gap to 0.15rem,
  `.comment-children` margin+padding down to 0.25rem combined —
  `.collapse-toggle` shrinks to match the rail, with its coarse-pointer tap
  target, see above, unaffected since that's a separate hit-area overlay).
  Measured on the actual deep fixture thread: the sixth-level comment sits
  at a fixed ~112px from the content edge regardless of viewport width
  (it's all rem/px, not viewport-relative), which is ~29% of a 390px-wide
  screen — under the 30% budget. No separate "continue thread" affordance
  was added on top of that: it's a documented follow-up if real content
  ever nests meaningfully deeper than the current fixture, at which point
  capping the *visual* depth (rendering anything past some max nesting at
  the same indent, with a "N more replies" link — the existing per-node
  collapse toggle already provides the mechanism, just not an automatic
  depth-triggered version of it) is the next step.
  **Implementation note:** the mobile shrink rule has to be the last
  `.comment-rail`/`.comment-children`/`.collapse-toggle` rule in
  `index.css` (it lives at the very end of the file, not next to the rest
  of the mobile-nav CSS) — a `@media` block earlier in source order does
  *not* win a cascade tie against a later unconditional rule for the same
  selector, and the unconditional comment-tree rules are defined well
  after where the mobile section would otherwise naturally sit.
- **Composer inputs on iOS**: every text input/textarea is forced to 16px
  under 760px regardless of its desktop size, because iOS Safari zooms the
  viewport on focusing anything smaller — composers need to stay usable
  without fighting that zoom. The new-post composer's fields
  (`.composer-field input`, `.composer-body`) get this override inside
  `~/components/composer/composer.css` itself rather than index.css's
  shared mobile section — that file's own unconditional 0.9rem sizing
  loads as a separate stylesheet and isn't guaranteed to lose a same-
  selector, same-specificity cascade tie against a rule in index.css (load
  order between per-component CSS files and index.css depends on which
  component mounts first, not file position), so the override has to live
  wherever the rule it's overriding does.

## 9. Adding to this system

- New metadata anywhere → add its selector to `index.css`'s "metadata is
  mono" rule; don't hand-roll a one-off style.
- New button → decide primary/secondary/ghost/destructive per §4/§6; don't
  reach for the gradient unless it's genuinely a primary submit action (and
  note that doing so keeps it at *more* than four gradient usages, which
  means updating this doc's §4, not just shipping it).
- New unread indicator → `--ember`, nothing else, full stop (§5).
