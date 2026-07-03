// Component tests for the AppShell bell (task C4, PLANDOC.md §7). `~/lib/api`
// is mocked outright (session.test.tsx's convention) for deterministic
// control over the poller's two calls (unread probe + latest preview).
import { Route, Router } from "@solidjs/router";
import { render, screen, waitFor } from "@solidjs/testing-library";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Notification, Thread, UserProfile } from "~/gen/types.gen";
import { SessionProvider } from "~/lib/session";

const { whoami, listNotifications, markNotificationRead, getThread, listBoards } = vi.hoisted(() => ({
  whoami: vi.fn(),
  listNotifications: vi.fn(),
  markNotificationRead: vi.fn(),
  getThread: vi.fn(),
  listBoards: vi.fn(),
}));

vi.mock("~/lib/api", () => ({
  api: {
    auth: { whoami },
    notification: { listNotifications, markNotificationRead },
    thread: { getThread },
    board: { listBoards },
  },
}));

const { default: NotificationBell } = await import("./NotificationBell");

const USER: UserProfile = {
  id: "01FPTESTUSER",
  linkkeysDomain: "example.com",
  handle: "alice",
  displayName: "Alice",
  kind: "human",
  roles: [],
  createdAt: new Date("2026-01-01T00:00:00Z"),
};

function notif(overrides: Partial<Notification>): Notification {
  return {
    id: "n-1",
    event: "new_comment",
    actorId: "bob-id",
    targetType: "post",
    targetId: "post-1",
    postId: "post-1",
    createdAt: new Date("2026-07-01T00:00:00Z"),
    ...overrides,
  };
}

function threadFor(): Thread {
  return {
    post: {
      id: "post-1",
      boardId: "board-1",
      authorId: "someone",
      title: "Welcome to Firepit",
      bodyMd: "body",
      origin: "user",
      commentCount: 0,
      lastActivityAt: new Date("2026-01-01T00:00:00Z"),
      createdAt: new Date("2026-01-01T00:00:00Z"),
    },
    comments: [],
  };
}

function renderBell() {
  window.history.pushState({}, "", "/");
  // The bell mounted as `root` (persists across route changes, like in
  // AppShell.tsx) rather than as the routed content itself — otherwise
  // navigating away on click-through would unmount it before we can assert
  // on its post-navigation state.
  return render(() => (
    <SessionProvider>
      <Router root={(p) => (
        <>
          <NotificationBell />
          {p.children}
        </>
      )}>
        <Route path="/" component={() => <p>home</p>} />
        <Route path="/b/:slug/p/:postId" component={() => <p>thread view</p>} />
      </Router>
    </SessionProvider>
  ));
}

beforeEach(() => {
  whoami.mockReset().mockResolvedValue(USER);
  markNotificationRead.mockReset().mockResolvedValue({});
  getThread.mockReset().mockResolvedValue(threadFor());
  listBoards.mockReset().mockResolvedValue({ boards: [{ id: "board-1", slug: "firepit", title: "Firepit Meta", kind: "discussion", createdBy: "x", createdAt: new Date() }] });
});

describe("NotificationBell", () => {
  it("shows an unread count once the poller resolves, and stays quiet when there's nothing unread", async () => {
    listNotifications.mockImplementation(({ unreadOnly }: { unreadOnly?: boolean }) =>
      Promise.resolve({ notifications: unreadOnly ? [notif({}), notif({ id: "n-2" })] : [notif({})] }),
    );

    renderBell();

    await waitFor(() => expect(screen.getByText("2")).toBeInTheDocument());
    expect(screen.getByRole("button", { name: /Notifications — 2 unread/ })).toBeInTheDocument();
  });

  it("selecting a notification from the dropdown marks it read and the bell's count updates", async () => {
    let unreadCount = 1;
    listNotifications.mockImplementation(({ unreadOnly }: { unreadOnly?: boolean }) =>
      Promise.resolve({
        notifications: unreadOnly ? Array.from({ length: unreadCount }, (_, i) => notif({ id: `n-${i}` })) : [notif({})],
      }),
    );
    const user = userEvent.setup();

    renderBell();
    await waitFor(() => expect(screen.getByText("1")).toBeInTheDocument());

    await user.click(screen.getByRole("button", { name: /Notifications/ }));
    await waitFor(() => expect(screen.getByText(/New reply/)).toBeInTheDocument());

    unreadCount = 0; // markNotificationRead "took effect" server-side by the time the poller refetches
    await user.click(screen.getByText(/New reply/));

    await waitFor(() => expect(markNotificationRead).toHaveBeenCalledWith(["n-1"]));
    await waitFor(() => expect(screen.queryByText("1")).not.toBeInTheDocument());
    expect(screen.getByRole("button", { name: "Notifications" })).toBeInTheDocument();
  });
});
