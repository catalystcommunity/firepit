// Route-level integration test (task C3 accept criteria: "permalink scroll
// target exists" + the auto-watermark read mark). Renders the real `App`
// (same convention as App.test.tsx) against the mock transport so routing,
// session, and the thread view all wire up exactly as they do in the
// browser — only the two behaviors CommentTree.test.tsx/Endorsements.test.tsx/
// RevisionHistory.test.tsx don't already cover at the component level.
import { render, screen, waitFor } from "@solidjs/testing-library";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import App from "~/App";
import { api } from "~/lib/api";
import { comments, MOCK_USER, posts } from "~/lib/mock/fixtures";

const welcomePost = posts.find((p) => p.title === "Welcome to Firepit");
if (!welcomePost) throw new Error("fixture missing the welcome post");
const welcomeComments = comments.filter((c) => c.postId === welcomePost.id);
const deepestComment = welcomeComments[welcomeComments.length - 1];

beforeEach(() => {
  window.history.pushState({}, "", "/");
});

afterEach(async () => {
  vi.useRealTimers();
  // Sessions persist (sessionStorage) across tests in this file since
  // `~/lib/api`'s client is a module singleton — leave each test logged out
  // for the next one, mirroring session.tsx's own logout().
  await api.auth.logout({});
});

describe("ThreadPage", () => {
  it("renders the post header, body, and full comment tree", async () => {
    window.history.pushState({}, "", `/b/firepit/p/${welcomePost.id}`);
    render(() => <App />);

    await waitFor(() => expect(screen.getByRole("heading", { name: "Welcome to Firepit" })).toBeInTheDocument());
    for (const c of welcomeComments) {
      expect(document.getElementById(`c-${c.id}`)).not.toBeNull();
    }
    // Logged out: a login prompt (in the thread body, not just the top bar)
    // instead of the reply composer.
    expect(screen.getByText(/to reply, endorse, or subscribe/)).toBeInTheDocument();
  });

  it("scrolls to and highlights the comment named in the URL's #c-<id> hash", async () => {
    window.history.pushState({}, "", `/b/firepit/p/${welcomePost.id}#c-${deepestComment.id}`);
    render(() => <App />);

    const target = await waitFor(() => {
      const el = document.getElementById(`c-${deepestComment.id}`);
      expect(el).not.toBeNull();
      return el as HTMLElement;
    });
    await waitFor(() => expect(target.classList.contains("permalink-highlight")).toBe(true));
  });

  it("auto-marks the post's read watermark ~2s after opening, once logged in", async () => {
    await api.auth.beginLogin({ domain: MOCK_USER.linkkeysDomain });
    const markReadSpy = vi.spyOn(api.read, "markRead");

    window.history.pushState({}, "", `/b/firepit/p/${welcomePost.id}`);
    render(() => <App />);

    // Wait for the session to resolve logged-in (real timers) before
    // switching to fake ones to control the 2s watermark delay precisely.
    await waitFor(() => expect(screen.queryByText(/to reply, endorse, or subscribe/)).toBeNull());
    expect(markReadSpy).not.toHaveBeenCalled();

    vi.useFakeTimers();
    await vi.advanceTimersByTimeAsync(2100);

    expect(markReadSpy).toHaveBeenCalledWith({ targetType: "post", targetId: welcomePost.id });
  });
});
