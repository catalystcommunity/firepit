// The "Blue Flame" signature mark (docs/DESIGN.md "signature motif"): a
// small teardrop/flame glyph filled with the signature gradient, sitting
// left of the "Firepit" wordmark in the topbar. This is one of exactly four
// places the gradient is allowed to appear — see docs/DESIGN.md — so it
// must not be reused as generic decoration elsewhere.
import type { Component } from "solid-js";

const FlameMark: Component = () => (
  <svg class="flame-mark" width="18" height="18" viewBox="0 0 24 24" aria-hidden="true">
    <defs>
      <linearGradient id="flame-mark-gradient" x1="0%" y1="0%" x2="100%" y2="100%">
        <stop offset="0%" style={{ "stop-color": "var(--accent)" }} />
        <stop offset="100%" style={{ "stop-color": "var(--accent-hi)" }} />
      </linearGradient>
    </defs>
    <path
      fill="url(#flame-mark-gradient)"
      d="M12 1.5c-.35 0-.68.17-.87.46C8.7 6.06 6 10.02 6 13.5 6 18.47 9.58 22 12 22s6-3.53 6-8.5c0-3.13-2.06-6.51-3.98-9.28a1.05 1.05 0 0 0-1.66-.08c-.53.63-1.2 1.5-1.86 2.5-.24-1.08-.63-2.15-1.19-3.16a1.04 1.04 0 0 0-.31-1.02z"
    />
  </svg>
);

export default FlameMark;
