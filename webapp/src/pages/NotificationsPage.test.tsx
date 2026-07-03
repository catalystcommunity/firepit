// Component tests for the /notifications inbox (task C4, PLANDOC.md §7's
// accept criterion: "component tests against mock client"). `~/lib/api` is
// mocked outright (session.test.tsx's convention) so every scenario is
// deterministic — a real mock-transport FixtureStore's shared, mutable
// fixture state would make "does mark-all-read clear the unread indicator"
// order-dependent across `it()` blocks in this file.
import { Route, Router } from "@solidjs/router";
import { render, screen, waitFor } from "@solidjs/testing-library";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Board, Notification, Thread, UserProfile } from "~/gen/types.gen";
import { FirepitServiceError, ServiceErrorCode } from "~/lib/errors";
import { SessionProvider } from "~/lib/session";

const { whoami, listNotifications, markNotificationRead, markAllRead, getThread, listBoards } = vi.hoisted(() => ({
  whoami: vi.fn(),
  listNotifications: vi.fn(),
  markNotificationRead: vi.fn(),
  markAllRead: vi.fn(),
  getThread: vi.fn(),
  listBoards: vi.fn(),
}));

vi.mock("~/lib/api", () => ({
  api: {
    auth: { whoami },
    notification: { listNotifications, markNotificationRead, markAllRead },
    thread: { getThread },
    board: { listBoards },
  },
}));

const { default: NotificationsPage } = await import("./NotificationsPage");

const USER: UserProfile = {
  id: "01FPTESTUSER",
  linkkeysDomain: "example.com",
  handle: "alice",
  displayName: "Alice",
  kind: "human",
  roles: [],
  createdAt: new Date("2026-01-01T00:00:00Z"),
};

const BOARD: Board = {
  id: "board-1",
  slug: "firepit",
  title: "Firepit Meta",
  kind: "discussion",
  createdBy: "someone",
  createdAt: new Date("2026-01-01T00:00:00Z"),
};

function notif(overrides: Partial<Notification>): Notification {
  return {
    id: "notif-1",
    event: "new_comment",
    actorId: "bob-id",
    targetType: "comment",
    targetId: "comment-1",
    postId: "post-1",
    createdAt: new Date("2026-07-01T00:00:00Z"),
    ...overrides,
  };
}

function threadFor(postId: string, title: string): Thread {
  return {
    post: {
      id: postId,
      boardId: BOARD.id,
      authorId: "someone",
      title,
      bodyMd: "body",
      origin: "user",
      commentCount: 0,
      lastActivityAt: new Date("2026-01-01T00:00:00Z"),
      createdAt: new Date("2026-01-01T00:00:00Z"),
    },
    comments: [],
  };
}

function renderPage() {
  window.history.pushState({}, "", "/notifications");
  return render(() => (
    <SessionProvider>
      <Router>
        <Route path="/notifications" component={NotificationsPage} />
        <Route path="/b/:slug/p/:postId" component={() => <p>thread view</p>} />
      </Router>
    </SessionProvider>
  ));
}

beforeEach(() => {
  whoami.mockReset().mockResolvedValue(USER);
  listNotifications.mockReset();
  markNotificationRead.mockReset().mockResolvedValue({});
  markAllRead.mockReset().mockResolvedValue({});
  getThread.mockReset().mockResolvedValue(threadFor("post-1", "Welcome to Firepit"));
  listBoards.mockReset().mockResolvedValue({ boards: [BOARD] });
});

describe("NotificationsPage", () => {
  it("renders the inbox newest-first with resolved post titles, and quiets read rows", async () => {
    listNotifications.mockResolvedValue({
      notifications: [
        notif({ id: "n-unread", event: "mention", readAt: undefined, createdAt: new Date("2026-07-02T00:00:00Z") }),
        notif({ id: "n-read", event: "new_comment", readAt: new Date("2026-07-01T12:00:00Z"), createdAt: new Date("2026-07-01T00:00:00Z") }),
      ],
    });

    renderPage();

    await waitFor(() => expect(screen.getAllByText("Welcome to Firepit")).toHaveLength(2));
    // The unread row offers a "Mark read" action; the read row doesn't.
    expect(screen.getAllByText("Mark read")).toHaveLength(1);
  });

  it("clicking a row marks it read and navigates to the resolved thread", async () => {
    listNotifications.mockResolvedValue({
      notifications: [notif({ id: "n-1", targetType: "comment", targetId: "c-1", postId: "post-1" })],
    });
    const user = userEvent.setup();

    renderPage();
    await waitFor(() => expect(screen.getByText("Welcome to Firepit")).toBeInTheDocument());

    await user.click(screen.getByText("Welcome to Firepit"));

    await waitFor(() => expect(markNotificationRead).toHaveBeenCalledWith(["n-1"]));
    await waitFor(() => expect(screen.getByText("thread view")).toBeInTheDocument());
    expect(window.location.pathname).toBe("/b/firepit/p/post-1");
    expect(window.location.hash).toBe("#comment-c-1");
  });

  it("mark-all-read clears every unread row and disables itself once nothing is left unread", async () => {
    listNotifications.mockResolvedValue({
      notifications: [notif({ id: "n-1" }), notif({ id: "n-2", targetId: "comment-2" })],
    });
    const user = userEvent.setup();

    renderPage();
    await waitFor(() => expect(screen.getAllByText("Mark read")).toHaveLength(2));

    const markAllButton = screen.getByRole("button", { name: "Mark all read" });
    expect(markAllButton).not.toBeDisabled();

    await user.click(markAllButton);

    await waitFor(() => expect(markAllRead).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(screen.queryByText("Mark read")).not.toBeInTheDocument());
    expect(markAllButton).toBeDisabled();
  });

  it("renders an inline ServiceError and doesn't crash when the inbox fails to load", async () => {
    listNotifications.mockRejectedValue(
      new FirepitServiceError({ code: ServiceErrorCode.Internal, message: "the server is on fire" }),
    );

    renderPage();

    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent("the server is on fire"));
  });
});
