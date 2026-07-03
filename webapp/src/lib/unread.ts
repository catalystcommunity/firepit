// Shared unread-summary poller (task C2, PLANDOC.md §7: "small shared poller
// util... expose a signal C4's bell could also read"). Polls
// `ReadService.unread-summary` every 60s while the caller is authenticated,
// plus once on every `window` focus event and immediately on any
// login/logout transition, and exposes the latest `UnreadSummary` as a
// Solid signal.
//
// Interface is intentionally tiny: `startUnreadPoller` + the `UnreadPoller`
// it returns (`summary`/`refresh`/`stop`), plus two pure read-helpers
// (`boardUnreadCount`, `postIsUnread`) for turning a summary into a per-item
// boolean/count without every caller re-deriving the same `.find()`. C2 uses
// this from `Home.tsx` (board index dots), `BoardPage.tsx` (post-row dots),
// and `AppShell.tsx`'s board rail; C4's notification bell is expected to
// call `startUnreadPoller` again for its own badge rather than reach into
// one of C2's instances — see the "safe to call more than once" note below.
import { createEffect, createSignal, on, onCleanup, type Accessor } from "solid-js";
import type { PostID, UnreadSummary } from "~/gen/types.gen";
import { api } from "./api";

const POLL_INTERVAL_MS = 60_000;

export interface UnreadPoller {
  /** Latest fetched summary, or `null` before the first successful poll (or while logged out). */
  summary: Accessor<UnreadSummary | null>;
  /** Force an immediate refresh — call after an action that changes read state (mark-read, subscribe, ...). */
  refresh: () => Promise<void>;
  /** Stop the interval and remove the focus listener. Called automatically on owner cleanup; exposed for callers that need to stop it early. */
  stop: () => void;
}

/**
 * Start polling `unread-summary`. Must be called from within a reactive
 * owner (a Solid component body) — it registers `onCleanup` so the interval
 * and focus listener are torn down when that owner is disposed.
 *
 * `isAuthenticated` is a reactive accessor (pass `() => session.user() !==
 * null`) so polling naturally starts/stops as login state changes, without
 * the caller having to re-wire anything on login/logout.
 *
 * Safe to call more than once per page load (e.g. once from the board rail,
 * once from a page body, once from C4's bell): each call gets its own
 * interval and signal. There is no cross-instance de-duplication — the
 * request here is one small poll a minute per caller, so an independent
 * instance per consumer is simpler than a shared singleton with
 * refcounting, and keeps this module's interface (and every consumer's
 * mental model) to "start one, read its signal."
 */
export function startUnreadPoller(isAuthenticated: Accessor<boolean>): UnreadPoller {
  const [summary, setSummary] = createSignal<UnreadSummary | null>(null);

  const poll = async (): Promise<void> => {
    if (!isAuthenticated()) {
      setSummary(null);
      return;
    }
    try {
      const result = await api.read.unreadSummary({});
      setSummary(result);
    } catch {
      // Transient failure (network blip, session expired mid-flight, ...) —
      // keep the last known summary rather than flashing every unread dot
      // away until the next successful poll.
    }
  };

  // `on(isAuthenticated, ...)` (not a bare `void poll()`) so a login/logout
  // that happens *after* this poller starts (e.g. AppShell's instance, which
  // mounts once for the whole app lifetime before `whoami` on boot has
  // necessarily resolved) re-polls immediately instead of waiting for the
  // next 60s tick or a window focus event.
  createEffect(on(isAuthenticated, () => void poll()));
  const interval = setInterval(() => void poll(), POLL_INTERVAL_MS);
  const onFocus = (): void => void poll();
  if (typeof window !== "undefined") window.addEventListener("focus", onFocus);

  const stop = (): void => {
    clearInterval(interval);
    if (typeof window !== "undefined") window.removeEventListener("focus", onFocus);
  };
  onCleanup(stop);

  return { summary, refresh: poll, stop };
}

/** How many unread posts `summary` reports for one board (0 if none, or before the first poll). */
export function boardUnreadCount(summary: UnreadSummary | null, boardId: string): number {
  return summary?.boards.find((b) => b.boardId === boardId)?.unreadCount ?? 0;
}

/** Whether `summary` lists `postId` as carrying unread activity, in any board. */
export function postIsUnread(summary: UnreadSummary | null, postId: PostID): boolean {
  return summary?.boards.some((b) => b.unreadPostIds.includes(postId)) ?? false;
}
