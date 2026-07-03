// Component tests for /settings' subscriptions-management section (task C4,
// PLANDOC.md §7's accept criterion "subscription mute toggle").
import { render, screen, waitFor, within } from "@solidjs/testing-library";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Board, Subscription, SubscriptionList, Thread } from "~/gen/types.gen";

const { listSubscriptions, setMuted, unsubscribe, listBoards, getThread } = vi.hoisted(() => ({
  listSubscriptions: vi.fn(),
  setMuted: vi.fn(),
  unsubscribe: vi.fn(),
  listBoards: vi.fn(),
  getThread: vi.fn(),
}));

vi.mock("~/lib/api", () => ({
  api: {
    subscription: { listSubscriptions, setMuted, unsubscribe },
    board: { listBoards },
    thread: { getThread },
  },
}));

const { default: SubscriptionsSection } = await import("./SubscriptionsSection");

const BOARD: Board = { id: "board-1", slug: "firepit", title: "Firepit Meta", kind: "discussion", createdBy: "x", createdAt: new Date() };

const BOARD_SUB: Subscription = { id: "sub-board", targetType: "board", targetId: "board-1", muted: false, createdAt: new Date("2026-01-01T00:00:00Z") };
const POST_SUB: Subscription = { id: "sub-post", targetType: "post", targetId: "post-1", muted: false, createdAt: new Date("2026-01-01T00:00:00Z") };

const THREAD: Thread = {
  post: {
    id: "post-1",
    boardId: "board-1",
    authorId: "someone",
    title: "v0.1.0 released",
    bodyMd: "body",
    origin: "system",
    commentCount: 0,
    lastActivityAt: new Date(),
    createdAt: new Date(),
  },
  comments: [],
};

const SEEDED: SubscriptionList = { subscriptions: [BOARD_SUB, POST_SUB] };

beforeEach(() => {
  listSubscriptions.mockReset().mockResolvedValue(SEEDED);
  listBoards.mockReset().mockResolvedValue({ boards: [BOARD] });
  getThread.mockReset().mockResolvedValue(THREAD);
  setMuted.mockReset();
  unsubscribe.mockReset();
});

describe("SubscriptionsSection", () => {
  it("groups subscriptions by target type with resolved board/post titles", async () => {
    render(() => <SubscriptionsSection />);

    await waitFor(() => expect(screen.getByText("Firepit Meta")).toBeInTheDocument());
    await waitFor(() => expect(screen.getByText("v0.1.0 released")).toBeInTheDocument());
    expect(screen.getByText("Boards")).toBeInTheDocument();
    expect(screen.getByText("Posts")).toBeInTheDocument();
  });

  it("muting a subscription calls set-muted and flips the button to Unmute", async () => {
    setMuted.mockResolvedValue({ ...BOARD_SUB, muted: true });
    const user = userEvent.setup();

    render(() => <SubscriptionsSection />);
    await waitFor(() => expect(screen.getByText("Firepit Meta")).toBeInTheDocument());

    const row = screen.getByText("Firepit Meta").closest("li") as HTMLElement;
    await user.click(within(row).getByRole("button", { name: "Mute" }));

    await waitFor(() =>
      expect(setMuted).toHaveBeenCalledWith({ targetType: "board", targetId: "board-1", muted: true }),
    );
    await waitFor(() => expect(screen.getByText("Firepit Meta").closest("li")).toHaveTextContent("Unmute"));
  });

  it("unsubscribing removes the row", async () => {
    unsubscribe.mockResolvedValue({});
    const user = userEvent.setup();

    render(() => <SubscriptionsSection />);
    await waitFor(() => expect(screen.getByText("v0.1.0 released")).toBeInTheDocument());

    const row = screen.getByText("v0.1.0 released").closest("li") as HTMLElement;
    await user.click(within(row).getByRole("button", { name: "Unsubscribe" }));

    await waitFor(() =>
      expect(unsubscribe).toHaveBeenCalledWith({ targetType: "post", targetId: "post-1" }),
    );
    await waitFor(() => expect(screen.queryByText("v0.1.0 released")).not.toBeInTheDocument());
  });
});
