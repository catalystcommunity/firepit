// Covers task C2's post-list accept criteria: cursor pagination appends
// (doesn't replace) the prior page, an unread post is flagged, and a
// GitHub-origin post gets its small origin glyph. `~/lib/api` is mocked
// outright (same pattern as `~/lib/session.test.tsx`) so pagination is
// driven by an explicit, deterministic `listPosts` implementation rather
// than depending on the mock transport's own fixture volume.
import { MemoryRouter, Route } from "@solidjs/router";
import { fireEvent, render, screen, waitFor } from "@solidjs/testing-library";
import type { JSX } from "solid-js";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ListPostsRequest, Post, UnreadSummary } from "~/gen/types.gen";
import { FirepitServiceError, ServiceErrorCode } from "~/lib/errors";

const { whoami, listPosts } = vi.hoisted(() => ({ whoami: vi.fn(), listPosts: vi.fn() }));

vi.mock("~/lib/api", () => ({
  api: { auth: { whoami }, thread: { listPosts } },
}));

const { SessionProvider } = await import("~/lib/session");
const { default: PostList } = await import("./PostList");

const unauthenticated = () =>
  Promise.reject(new FirepitServiceError({ code: ServiceErrorCode.Unauthenticated, message: "no active session" }));

const post = (id: string, overrides: Partial<Post> = {}): Post => ({
  id,
  boardId: "b1",
  authorId: "01FPMOCKUSERBOB00000000",
  title: `Post ${id}`,
  bodyMd: "body",
  origin: "user",
  commentCount: 0,
  lastActivityAt: new Date("2026-07-01T00:00:00Z"),
  createdAt: new Date("2026-07-01T00:00:00Z"),
  ...overrides,
});

// PostList's rows render an `<A>` to the thread route, which needs a Router context.
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
  listPosts.mockReset();
  whoami.mockImplementation(unauthenticated);
});

describe("PostList", () => {
  it("loads the first page, then appends the next page on 'Load more'", async () => {
    listPosts.mockImplementation((req: ListPostsRequest) => {
      if (!req.cursor) return Promise.resolve({ posts: [post("1"), post("2")], nextCursor: "2" });
      if (req.cursor === "2") return Promise.resolve({ posts: [post("3")], nextCursor: undefined });
      throw new Error(`unexpected cursor: ${req.cursor}`);
    });

    renderWithRouter(() => <PostList boardId="b1" boardSlug="board" summary={() => null} />);

    await waitFor(() => expect(screen.getByText("Post 1")).toBeInTheDocument());
    expect(screen.getByText("Post 2")).toBeInTheDocument();
    expect(screen.queryByText("Post 3")).toBeNull();
    expect(listPosts).toHaveBeenCalledWith({ boardId: "b1", cursor: undefined, limit: 20 });

    fireEvent.click(screen.getByRole("button", { name: "Load more" }));

    await waitFor(() => expect(screen.getByText("Post 3")).toBeInTheDocument());
    // The first page's rows are still there — appended, not replaced.
    expect(screen.getByText("Post 1")).toBeInTheDocument();
    expect(screen.getByText("Post 2")).toBeInTheDocument();
    expect(listPosts).toHaveBeenCalledWith({ boardId: "b1", cursor: "2", limit: 20 });
    // No more pages: the "Load more" control goes away.
    expect(screen.queryByRole("button", { name: "Load more" })).toBeNull();
  });

  it("flags an unread post per the summary and shows a glyph on a GitHub-origin post", async () => {
    listPosts.mockResolvedValue({
      posts: [post("gh", { origin: "github", title: "GH post" }), post("normal", { title: "Normal post" })],
      nextCursor: undefined,
    });
    const summary: UnreadSummary = { boards: [{ boardId: "b1", unreadCount: 1, unreadPostIds: ["gh"] }] };

    renderWithRouter(() => <PostList boardId="b1" boardSlug="board" summary={() => summary} />);

    await waitFor(() => expect(screen.getByText("GH post")).toBeInTheDocument());

    const ghRow = screen.getByText("GH post").closest("li");
    const normalRow = screen.getByText("Normal post").closest("li");
    expect(ghRow?.classList.contains("unread")).toBe(true);
    expect(ghRow?.querySelector(".origin-glyph")).not.toBeNull();
    expect(normalRow?.classList.contains("unread")).toBe(false);
    expect(normalRow?.querySelector(".origin-glyph")).toBeNull();
  });

  it("shows an empty-state message when the board has no posts", async () => {
    listPosts.mockResolvedValue({ posts: [], nextCursor: undefined });

    renderWithRouter(() => <PostList boardId="b1" boardSlug="board" summary={() => null} />);

    await waitFor(() => expect(screen.getByText(/No posts yet/)).toBeInTheDocument());
  });
});
