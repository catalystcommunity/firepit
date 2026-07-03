// Shared utilities for the notification bell (src/components/notifications/
// NotificationBell.tsx) and the /notifications inbox (src/pages/
// NotificationsPage.tsx) — task C4, PLANDOC.md §7.
//
// Two unrelated things live here rather than in either component:
//
// 1. `createNotificationsPoller` — a tiny, self-contained unread poller
//    (60s interval + refresh-on-focus). PLANDOC.md's C2 (board rail) needs
//    its own poll against ReadService's `unread-summary` for board/post
//    badges — a *different* endpoint, but the same "interval + focus"
//    shape. If C2's worktree already grew its own `src/lib/unread.ts` for
//    that, unifying the two into one generic poller is a reasonable
//    merge-time follow-up; not attempted here since C2 isn't visible from
//    this worktree.
// 2. Post/board resolution for turning a bare `Notification` (which only
//    carries ids) into something a UI can show and link to. CSIL has no
//    "get post by id" or "resolve user id -> handle" op (see src/lib/mock/
//    fixtures.ts's own note on the same gap for post authors/endorsers) —
//    `createPostSummaryResolver` below is the best a client can do:
//    ThreadService.getThread for the post's title + board id, then
//    BoardService.listBoards (cached) to turn that board id into the slug
//    the router needs. `actorLabel` is the equivalent honest fallback for
//    "who did this" — there's no display name to show for anyone but the
//    caller, so it shows "you", a truncated id, or "the project" (no actor
//    at all — a system-authored event) rather than pretending otherwise.
import { createEffect, createSignal, onCleanup, type Accessor } from "solid-js";
import type { Board, Notification, NotificationEvent } from "~/gen/types.gen";
import { api } from "./api";

/** A quiet glyph per event type — a legend, not a severity indicator (design direction:
 * notifications are a curated signal channel, not an engagement surface). */
export const NOTIFICATION_GLYPH: Record<NotificationEvent, string> = {
  new_post: "📝",
  new_comment: "💬",
  mention: "@",
  github_event: "🐙",
};

export const NOTIFICATION_LABEL: Record<NotificationEvent, string> = {
  new_post: "New post",
  new_comment: "New reply",
  mention: "Mention",
  github_event: "GitHub activity",
};

/** Coarse, low-drama relative time — "just now" / "5m ago" / "3h ago" / "2d ago", falling
 * back to a plain date once it's stopped being "recent" in any useful sense. */
export function relativeTime(date: Date, now: Date = new Date()): string {
  const minutes = Math.floor((now.getTime() - date.getTime()) / 60_000);
  if (minutes < 1) return "just now";
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  if (days < 30) return `${days}d ago`;
  return date.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
}

/** "you" for the viewer's own actions, a short truncated id otherwise, "the project" when
 * there's no actor at all (a system-authored event). See the module doc: there is no
 * user-lookup op in CSIL yet, so a real display name genuinely isn't available. */
export function actorLabel(actorId: string | undefined, viewerId: string | undefined): string {
  if (!actorId) return "the project";
  if (viewerId && actorId === viewerId) return "you";
  return actorId.length > 10 ? `${actorId.slice(0, 10)}…` : actorId;
}

export interface PostSummary {
  postId: string;
  title: string;
  boardId: string;
  boardSlug: string;
  /** True when the post no longer resolves (deleted, or the id is stale) — render a plain
   * fallback instead of a broken link (Notification's own doc: target ids aren't guaranteed
   * to still resolve). */
  unresolved: boolean;
}

/**
 * Builds a per-instance resolver (never a module-level singleton — a shared cache would
 * leak fixture state across tests the way mockTransport.test.ts's fresh-store-per-test
 * convention is designed to avoid). Callers create one per component mount and reuse it for
 * every notification/subscription row that component renders.
 */
export function createPostSummaryResolver() {
  const postCache = new Map<string, Promise<PostSummary>>();
  let boardsPromise: Promise<Board[]> | null = null;

  const loadBoards = (): Promise<Board[]> => {
    boardsPromise ??= api.board.listBoards({}).then((page) => page.boards);
    return boardsPromise;
  };

  const resolve = (postId: string): Promise<PostSummary> => {
    let cached = postCache.get(postId);
    if (!cached) {
      cached = (async () => {
        try {
          const [thread, boards] = await Promise.all([api.thread.getThread({ postId }), loadBoards()]);
          const board = boards.find((b) => b.id === thread.post.boardId);
          return {
            postId,
            title: thread.post.title,
            boardId: thread.post.boardId,
            boardSlug: board?.slug ?? thread.post.boardId,
            unresolved: false,
          };
        } catch {
          return { postId, title: "(unavailable)", boardId: "", boardSlug: "", unresolved: true };
        }
      })();
      postCache.set(postId, cached);
    }
    return cached;
  };

  return { resolve };
}

export type PostSummaryResolver = ReturnType<typeof createPostSummaryResolver>;

/**
 * Where a notification's click-through should navigate. A comment-target notification links
 * to the post with a `#comment-{id}` anchor — the same permalink convention C3's thread view
 * uses elsewhere (PLANDOC.md §7 C3: "permalinks to comments"); C3 hasn't landed in this
 * worktree, so the convention is documented here rather than exercised end-to-end against a
 * real thread view yet. `getThread` scrolling/highlighting a `location.hash` comment id is
 * the follow-up C3 (or a merge) needs to honor for this to do anything visible.
 */
export function notificationHref(n: Notification, summary: PostSummary): string {
  if (summary.unresolved) return "/notifications";
  const anchor = n.targetType === "comment" ? `#comment-${n.targetId}` : "";
  return `/b/${summary.boardSlug}/p/${n.postId}${anchor}`;
}

export interface NotificationsPollerOptions {
  intervalMs?: number;
  /** Page size for the unread probe. CSIL has no direct "unread count" op (just a list),
   * so the poller caps its probe at this many and reports "N+" once saturated rather than
   * guessing at a true total. */
  limit?: number;
  previewLimit?: number;
}

export interface NotificationsPoller {
  unreadCount: Accessor<number>;
  /** True once `unreadCount()` has hit the probe's `limit` — render as "N+", not exact. */
  saturated: Accessor<boolean>;
  latest: Accessor<Notification[]>;
  refresh: () => Promise<void>;
}

/**
 * The bell's poller: polls on a 60s interval and on window focus, gated by `enabled` (stop
 * entirely once logged out rather than repeatedly hitting Unauthenticated). Deliberately
 * tiny and independent of any unread-summary (board/post) poller C2 builds for the board
 * rail — see this module's doc comment for the planned merge-time unification.
 */
export function createNotificationsPoller(
  enabled: Accessor<boolean>,
  opts: NotificationsPollerOptions = {},
): NotificationsPoller {
  const intervalMs = opts.intervalMs ?? 60_000;
  const limit = opts.limit ?? 50;
  const previewLimit = opts.previewLimit ?? 5;

  const [unread, setUnread] = createSignal<Notification[]>([]);
  const [latest, setLatest] = createSignal<Notification[]>([]);

  const refresh = async (): Promise<void> => {
    if (!enabled()) return;
    try {
      const [unreadPage, latestPage] = await Promise.all([
        api.notification.listNotifications({ unreadOnly: true, limit }),
        api.notification.listNotifications({ limit: previewLimit }),
      ]);
      setUnread(unreadPage.notifications);
      setLatest(latestPage.notifications);
    } catch {
      // A transient poll failure just leaves this tick's numbers stale — no error UI for a
      // background poll (design direction: quiet, not alarming); the next interval or focus
      // event tries again.
    }
  };

  createEffect(() => {
    if (!enabled()) {
      setUnread([]);
      setLatest([]);
      return;
    }
    void refresh();
    const timer = setInterval(() => void refresh(), intervalMs);
    const onFocus = () => void refresh();
    window.addEventListener("focus", onFocus);
    onCleanup(() => {
      clearInterval(timer);
      window.removeEventListener("focus", onFocus);
    });
  });

  return {
    unreadCount: () => unread().length,
    saturated: () => unread().length >= limit,
    latest,
    refresh,
  };
}
