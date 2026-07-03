// Covers task C2's shared unread-summary poller: polls while authenticated,
// stays idle (and clears any summary) while not, re-polls on a 60s interval
// and on window focus, and the two pure per-item helpers. `~/lib/api` is
// mocked outright (same pattern as `~/lib/session.test.tsx`) so the interval
// behavior is deterministic under fake timers, independent of the mock
// transport's own timing.
import { createRoot, createSignal } from "solid-js";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { UnreadSummary } from "~/gen/types.gen";

const { unreadSummary } = vi.hoisted(() => ({ unreadSummary: vi.fn() }));

vi.mock("./api", () => ({
  api: { read: { unreadSummary } },
}));

const { startUnreadPoller, boardUnreadCount, postIsUnread } = await import("./unread");

const SUMMARY: UnreadSummary = { boards: [{ boardId: "b1", unreadCount: 2, unreadPostIds: ["p1", "p2"] }] };

beforeEach(() => {
  unreadSummary.mockReset();
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

describe("startUnreadPoller", () => {
  it("polls immediately when authenticated and exposes the result", async () => {
    unreadSummary.mockResolvedValue(SUMMARY);
    let dispose!: () => void;
    const poller = createRoot((d) => {
      dispose = d;
      return startUnreadPoller(() => true);
    });

    await vi.waitFor(() => expect(poller.summary()).toEqual(SUMMARY));
    expect(unreadSummary).toHaveBeenCalledTimes(1);
    dispose();
  });

  it("does not poll while unauthenticated, and reports no summary", async () => {
    let dispose!: () => void;
    const poller = createRoot((d) => {
      dispose = d;
      return startUnreadPoller(() => false);
    });

    await Promise.resolve();
    expect(unreadSummary).not.toHaveBeenCalled();
    expect(poller.summary()).toBeNull();
    dispose();
  });

  it("re-polls immediately when isAuthenticated flips true after the poller already started", async () => {
    // Regression coverage: a poller started before login resolves (e.g.
    // AppShell's, which mounts once for the whole app before whoami-on-boot
    // settles) must not get stuck reporting no-summary forever once the
    // caller does log in — it shouldn't have to wait for the 60s tick or a
    // focus event.
    unreadSummary.mockResolvedValue(SUMMARY);
    let dispose!: () => void;
    let setAuthenticated!: (v: boolean) => void;
    const poller = createRoot((d) => {
      dispose = d;
      const [authenticated, setter] = createSignal(false);
      setAuthenticated = setter;
      // Forwarded as-is into startUnreadPoller's own createEffect (unread.ts),
      // which is the tracked scope that actually reads it.
      // eslint-disable-next-line solid/reactivity
      return startUnreadPoller(() => authenticated());
    });

    await Promise.resolve();
    expect(unreadSummary).not.toHaveBeenCalled();
    expect(poller.summary()).toBeNull();

    setAuthenticated(true);
    await vi.waitFor(() => expect(poller.summary()).toEqual(SUMMARY));
    expect(unreadSummary).toHaveBeenCalledTimes(1);
    dispose();
  });

  it("polls again after the 60s interval elapses", async () => {
    unreadSummary.mockResolvedValue(SUMMARY);
    let dispose!: () => void;
    createRoot((d) => {
      dispose = d;
      return startUnreadPoller(() => true);
    });

    await vi.waitFor(() => expect(unreadSummary).toHaveBeenCalledTimes(1));
    await vi.advanceTimersByTimeAsync(60_000);
    expect(unreadSummary).toHaveBeenCalledTimes(2);
    dispose();
  });

  it("polls again on a window focus event", async () => {
    unreadSummary.mockResolvedValue(SUMMARY);
    let dispose!: () => void;
    createRoot((d) => {
      dispose = d;
      return startUnreadPoller(() => true);
    });

    await vi.waitFor(() => expect(unreadSummary).toHaveBeenCalledTimes(1));
    window.dispatchEvent(new Event("focus"));
    await vi.waitFor(() => expect(unreadSummary).toHaveBeenCalledTimes(2));
    dispose();
  });

  it("stop()/owner disposal removes the interval and focus listener", async () => {
    unreadSummary.mockResolvedValue(SUMMARY);
    let dispose!: () => void;
    const poller = createRoot((d) => {
      dispose = d;
      return startUnreadPoller(() => true);
    });
    await vi.waitFor(() => expect(unreadSummary).toHaveBeenCalledTimes(1));

    poller.stop();
    dispose();
    await vi.advanceTimersByTimeAsync(120_000);
    window.dispatchEvent(new Event("focus"));
    expect(unreadSummary).toHaveBeenCalledTimes(1);
  });
});

describe("boardUnreadCount", () => {
  it("returns the board's unread count", () => {
    expect(boardUnreadCount(SUMMARY, "b1")).toBe(2);
  });

  it("returns 0 for a board with no entry, or a null summary", () => {
    expect(boardUnreadCount(SUMMARY, "other-board")).toBe(0);
    expect(boardUnreadCount(null, "b1")).toBe(0);
  });
});

describe("postIsUnread", () => {
  it("is true only for a post id listed in any board's unreadPostIds", () => {
    expect(postIsUnread(SUMMARY, "p1")).toBe(true);
    expect(postIsUnread(SUMMARY, "p9")).toBe(false);
    expect(postIsUnread(null, "p1")).toBe(false);
  });
});
