// Covers task C2's composer accept criteria: anonymous visitors get a login
// prompt instead of the form, client-side validation blocks an empty
// title/body before ever calling the API, a `ServiceError` with `field` set
// renders inline next to that field (task C2's "field-level where
// error.field is set"), and a successful submit navigates to the new
// thread's route. `~/lib/api` is mocked outright (same pattern as
// `~/lib/session.test.tsx`).
import { createMemoryHistory, MemoryRouter, Route } from "@solidjs/router";
import { fireEvent, render, screen, waitFor } from "@solidjs/testing-library";
import type { JSX } from "solid-js";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Post, UserProfile } from "~/gen/types.gen";
import { FirepitServiceError, ServiceErrorCode } from "~/lib/errors";

const { whoami, createPost } = vi.hoisted(() => ({ whoami: vi.fn(), createPost: vi.fn() }));

vi.mock("~/lib/api", () => ({
  api: { auth: { whoami }, thread: { createPost } },
}));

const { SessionProvider } = await import("~/lib/session");
const { default: PostComposer } = await import("./PostComposer");
const submitButton = { name: "Start thread" };
const bodyPlaceholder = /Add background, links, decisions needed/;

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

function renderWithRouter(ui: () => JSX.Element) {
  const history = createMemoryHistory();
  render(() => (
    <SessionProvider>
      <MemoryRouter history={history}>
        <Route path="/" component={ui} />
      </MemoryRouter>
    </SessionProvider>
  ));
  return history;
}

beforeEach(() => {
  whoami.mockReset();
  createPost.mockReset();
});

describe("PostComposer", () => {
  it("shows a login prompt instead of the form when anonymous", async () => {
    whoami.mockImplementation(unauthenticated);
    renderWithRouter(() => <PostComposer boardId="b1" boardSlug="board" />);

    await waitFor(() => expect(screen.getByText(/Log in/)).toBeInTheDocument());
    expect(screen.queryByRole("textbox", { name: "Thread title" })).toBeNull();
    expect(createPost).not.toHaveBeenCalled();
  });

  it("blocks submit with an inline error when the title is empty, without calling the API", async () => {
    whoami.mockResolvedValue(USER);
    renderWithRouter(() => <PostComposer boardId="b1" boardSlug="board" />);

    await waitFor(() => expect(screen.getByRole("button", submitButton)).toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", submitButton));

    await waitFor(() => expect(screen.getByText("Title is required.")).toBeInTheDocument());
    expect(createPost).not.toHaveBeenCalled();
  });

  it("renders a ServiceError's message next to the field it names", async () => {
    whoami.mockResolvedValue(USER);
    createPost.mockRejectedValue(
      new FirepitServiceError({ code: ServiceErrorCode.Validation, field: "title", message: "That title is taken." }),
    );
    renderWithRouter(() => <PostComposer boardId="b1" boardSlug="board" />);

    await waitFor(() => expect(screen.getByRole("button", submitButton)).toBeInTheDocument());
    const titleInput = screen.getByRole("textbox", { name: "Thread title" });
    fireEvent.input(titleInput, { target: { value: "Duplicate title" } });
    fireEvent.input(screen.getByPlaceholderText(bodyPlaceholder), { target: { value: "some body text" } });
    fireEvent.click(screen.getByRole("button", submitButton));

    await waitFor(() => expect(screen.getByText("That title is taken.")).toBeInTheDocument());
    expect(titleInput).toHaveAttribute("aria-invalid", "true");
  });

  it("creates the post and navigates to its thread route on success", async () => {
    whoami.mockResolvedValue(USER);
    const created: Post = {
      id: "p9",
      boardId: "b1",
      authorId: USER.id,
      title: "A new thread",
      bodyMd: "hello there",
      origin: "user",
      commentCount: 0,
      lastActivityAt: new Date("2026-07-03T00:00:00Z"),
      createdAt: new Date("2026-07-03T00:00:00Z"),
    };
    createPost.mockResolvedValue(created);
    const onCreated = vi.fn();

    const history = renderWithRouter(() => <PostComposer boardId="b1" boardSlug="board" onCreated={onCreated} />);

    await waitFor(() => expect(screen.getByRole("button", submitButton)).toBeInTheDocument());
    fireEvent.input(screen.getByRole("textbox", { name: "Thread title" }), { target: { value: "A new thread" } });
    fireEvent.input(screen.getByPlaceholderText(bodyPlaceholder), { target: { value: "hello there" } });
    fireEvent.click(screen.getByRole("button", submitButton));

    await waitFor(() =>
      expect(createPost).toHaveBeenCalledWith({ boardId: "b1", title: "A new thread", bodyMd: "hello there" }),
    );
    await waitFor(() => expect(history.get()).toBe("/b/board/p/p9"));
    expect(onCreated).toHaveBeenCalledWith(created);
  });

  it("switches to the preview tab and renders sanitized markdown", async () => {
    whoami.mockResolvedValue(USER);
    renderWithRouter(() => <PostComposer boardId="b1" boardSlug="board" />);

    await waitFor(() => expect(screen.getByRole("button", submitButton)).toBeInTheDocument());
    fireEvent.input(screen.getByPlaceholderText(bodyPlaceholder), {
      target: { value: "**bold** <script>alert(1)</script>" },
    });
    fireEvent.click(screen.getByRole("tab", { name: "Preview" }));

    await waitFor(() => expect(screen.getByText("bold").tagName).toBe("STRONG"));
    expect(document.querySelector("script")).toBeNull();
  });
});
