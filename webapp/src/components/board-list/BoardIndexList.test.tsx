// Covers task C2's board index accept criteria: renders fixture-shaped
// boards grouped announce/discussion, the subscribe toggle round-trips, and
// an unread dot appears only for a board the (stubbed) unread summary
// flags. `~/lib/api` is mocked outright (same pattern as
// `~/lib/session.test.tsx`) so every scenario is deterministic and
// independent of the real mock transport's fixture content.
import { MemoryRouter, Route } from "@solidjs/router";
import { fireEvent, render, screen, waitFor, within } from "@solidjs/testing-library";
import type { JSX } from "solid-js";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Board, SubscriptionList, UserProfile } from "~/gen/types.gen";
import { FirepitServiceError, ServiceErrorCode } from "~/lib/errors";
import type { UnreadPoller } from "~/lib/unread";

const { whoami, listBoards, listSubscriptions, subscribe, unsubscribe } = vi.hoisted(() => ({
  whoami: vi.fn(),
  listBoards: vi.fn(),
  listSubscriptions: vi.fn(),
  subscribe: vi.fn(),
  unsubscribe: vi.fn(),
}));

vi.mock("~/lib/api", () => ({
  api: {
    auth: { whoami },
    board: { listBoards },
    subscription: { listSubscriptions, subscribe, unsubscribe, setMuted: vi.fn() },
  },
}));

const { SessionProvider } = await import("~/lib/session");
const { default: BoardIndexList } = await import("./BoardIndexList");

const unauthenticated = () =>
  Promise.reject(new FirepitServiceError({ code: ServiceErrorCode.Unauthenticated, message: "no active session" }));

const USER: UserProfile = {
  id: "u1",
  linkkeysDomain: "example.com",
  handle: "alice",
  displayName: "Alice",
  kind: "human",
  roles: [],
  createdAt: new Date("2026-01-01T00:00:00Z"),
};

const BOARD_ANNOUNCE: Board = {
  id: "b1",
  slug: "announce-board",
  title: "Announce Board",
  description: "Announcements only.",
  kind: "announce",
  createdBy: "u2",
  createdAt: new Date("2026-01-01T00:00:00Z"),
};

const BOARD_DISCUSSION: Board = {
  id: "b2",
  slug: "discuss-board",
  title: "Discuss Board",
  description: "General discussion.",
  kind: "discussion",
  createdBy: "u2",
  createdAt: new Date("2026-01-01T00:00:00Z"),
};

const stubPoller = (boards: { boardId: string; unreadCount: number; unreadPostIds: string[] }[]): UnreadPoller => ({
  summary: () => ({ boards }),
  refresh: async () => {},
  stop: () => {},
});

// BoardIndexList's rows render an `<A>` (subscribe login prompt links to
// "/login"), which requires a Router context — a `MemoryRouter` keeps that
// context local to the test instead of touching real browser history.
function renderWithRouter(ui: () => JSX.Element): void {
  render(() => (
    <SessionProvider>
      <MemoryRouter>
        <Route path="/" component={ui} />
      </MemoryRouter>
    </SessionProvider>
  ));
}

beforeEach(() => {
  whoami.mockReset();
  listBoards.mockReset();
  listSubscriptions.mockReset();
  subscribe.mockReset();
  unsubscribe.mockReset();
  listBoards.mockResolvedValue({ boards: [BOARD_ANNOUNCE, BOARD_DISCUSSION] });
});

describe("BoardIndexList", () => {
  it("renders the fixture boards grouped by kind, with title and description", async () => {
    whoami.mockImplementation(unauthenticated);
    renderWithRouter(() => <BoardIndexList poller={stubPoller([])} />);

    await waitFor(() => expect(screen.getByText("Announce Board")).toBeInTheDocument());
    expect(screen.getByText("Announcements")).toBeInTheDocument();
    expect(screen.getByText("Discussion")).toBeInTheDocument();
    expect(screen.getByText("Announcements only.")).toBeInTheDocument();
    expect(screen.getByText("Discuss Board")).toBeInTheDocument();
    expect(screen.getByText("General discussion.")).toBeInTheDocument();

    // Anonymous: a login prompt instead of a subscribe control, one per board.
    expect(screen.getAllByText("Log in to subscribe")).toHaveLength(2);
  });

  it("shows an unread dot only for the board the summary flags", async () => {
    whoami.mockImplementation(unauthenticated);
    renderWithRouter(() => (
      <BoardIndexList poller={stubPoller([{ boardId: "b1", unreadCount: 2, unreadPostIds: ["p1", "p2"] }])} />
    ));

    await waitFor(() => expect(screen.getByText("Announce Board")).toBeInTheDocument());
    const announceRow = screen.getByText("Announce Board").closest("li");
    const discussRow = screen.getByText("Discuss Board").closest("li");
    expect(announceRow?.querySelector(".unread-dot")).not.toBeNull();
    expect(discussRow?.querySelector(".unread-dot")).toBeNull();
  });

  it("subscribe toggle round-trips: subscribe, then unsubscribe", async () => {
    whoami.mockResolvedValue(USER);
    listSubscriptions.mockResolvedValue({ subscriptions: [] } satisfies SubscriptionList);
    subscribe.mockResolvedValue({
      id: "sub1",
      targetType: "board",
      targetId: "b1",
      muted: false,
      createdAt: new Date("2026-07-01T00:00:00Z"),
    });
    unsubscribe.mockResolvedValue({});

    renderWithRouter(() => <BoardIndexList poller={stubPoller([])} />);

    await waitFor(() => expect(screen.getByText("Announce Board")).toBeInTheDocument());
    const row = screen.getByText("Announce Board").closest("li") as HTMLElement;

    fireEvent.click(within(row).getByRole("button", { name: "Subscribe" }));
    await waitFor(() => expect(subscribe).toHaveBeenCalledWith({ targetType: "board", targetId: "b1" }));
    await waitFor(() => expect(within(row).getByRole("button", { name: "Subscribed" })).toBeInTheDocument());

    fireEvent.click(within(row).getByRole("button", { name: "Subscribed" }));
    await waitFor(() => expect(unsubscribe).toHaveBeenCalledWith({ targetType: "board", targetId: "b1" }));
    await waitFor(() => expect(within(row).getByRole("button", { name: "Subscribe" })).toBeInTheDocument());
  });

  it("starts a board already subscribed (per list-subscriptions) as 'Subscribed'", async () => {
    whoami.mockResolvedValue(USER);
    listSubscriptions.mockResolvedValue({
      subscriptions: [
        { id: "sub1", targetType: "board", targetId: "b1", muted: false, createdAt: new Date("2026-06-01T00:00:00Z") },
      ],
    } satisfies SubscriptionList);

    renderWithRouter(() => <BoardIndexList poller={stubPoller([])} />);

    await waitFor(() => expect(screen.getByText("Announce Board")).toBeInTheDocument());
    const row = screen.getByText("Announce Board").closest("li") as HTMLElement;
    expect(within(row).getByRole("button", { name: "Subscribed" })).toBeInTheDocument();
  });
});
