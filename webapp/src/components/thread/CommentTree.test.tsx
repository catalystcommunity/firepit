// Tree rendering, collapse, and the flat "mailing-list order" toggle (task
// C3 accept criteria: "deep tree render, collapse state"). Uses the real
// fixture thread (fixtures.ts's six-level chain, plus one root-level
// GitHub-origin comment) rather than hand-built test data, since it's the
// thread this feature was built to render.
import { createSignal } from "solid-js";
import { fireEvent, render, screen } from "@solidjs/testing-library";
import { describe, expect, it } from "vitest";
import { comments as fixtureComments, posts } from "~/lib/mock/fixtures";
import type { Comment } from "~/gen/types.gen";
import type { ThreadActions } from "./threadActions";
import type { ThreadViewMode } from "./viewMode";
import CommentTree from "./CommentTree";

const welcomePost = posts.find((p) => p.title === "Welcome to Firepit");
if (!welcomePost) throw new Error("fixture missing the welcome post");
const welcomeComments = fixtureComments.filter((c) => c.postId === welcomePost.id);

function stubActions(): ThreadActions {
  return {
    viewer: null,
    mentionCandidates: [],
    reply: async () => {},
    editComment: async () => {},
    deleteComment: async () => {},
    editPost: async () => {},
    deletePost: async () => {},
    isUnread: () => false,
    isUnreadOverride: () => false,
    toggleReadOverride: async () => {},
  };
}

function renderedCommentIds(): string[] {
  return [...document.querySelectorAll(".content-card.is-comment")].map((el) => el.id.replace(/^c-/, ""));
}

describe("CommentTree", () => {
  it("tree mode renders the whole thread depth-first, in server order", () => {
    render(() => <CommentTree comments={welcomeComments} mode="tree" viewer={null} ctx={stubActions()} />);
    expect(renderedCommentIds()).toEqual(welcomeComments.map((c) => c.id));
  });

  it("collapsing a subtree hides its descendants but not its siblings", () => {
    render(() => <CommentTree comments={welcomeComments} mode="tree" viewer={null} ctx={stubActions()} />);

    // The chain's root (first comment with replies) is the first
    // collapse-toggle in document order — the GitHub-origin comment ahead
    // of it is a childless root and gets no toggle.
    const toggles = screen.getAllByRole("button", { name: /collapse replies/i });
    fireEvent.click(toggles[0]);

    const idsAfterCollapse = renderedCommentIds();
    // The chain's root itself and everything before it (the GitHub comment)
    // still render...
    expect(idsAfterCollapse).toContain(welcomeComments[0].id);
    expect(idsAfterCollapse).toContain(welcomeComments[1].id);
    // ...but every descendant is hidden.
    for (const c of welcomeComments.slice(2)) {
      expect(idsAfterCollapse).not.toContain(c.id);
    }
    expect(screen.getByText(/replies hidden/)).toBeInTheDocument();

    // Expanding again restores the full order.
    fireEvent.click(screen.getByRole("button", { name: /expand replies/i }));
    expect(renderedCommentIds()).toEqual(welcomeComments.map((c) => c.id));
  });

  it('the flat "mailing-list order" toggle renders every comment, unnested, in the same server order', () => {
    function Harness() {
      const [mode, setMode] = createSignal<ThreadViewMode>("tree");
      return (
        <div>
          <button type="button" onClick={() => setMode((m) => (m === "tree" ? "flat" : "tree"))}>
            toggle view
          </button>
          <CommentTree comments={welcomeComments} mode={mode()} viewer={null} ctx={stubActions()} />
        </div>
      );
    }

    render(() => <Harness />);

    // Collapse the chain root in tree mode first...
    fireEvent.click(screen.getAllByRole("button", { name: /collapse replies/i })[0]);
    expect(renderedCommentIds().length).toBeLessThan(welcomeComments.length);

    // ...switching to flat view has no nested `<ul>`s and reveals every
    // comment regardless of the tree's collapsed state, in server order.
    fireEvent.click(screen.getByRole("button", { name: "toggle view" }));
    expect(document.querySelector(".comment-children")).toBeNull();
    expect(renderedCommentIds()).toEqual(welcomeComments.map((c) => c.id));
  });

  it("gives every comment a stable `#c-<id>` permalink anchor", () => {
    render(() => <CommentTree comments={welcomeComments} mode="tree" viewer={null} ctx={stubActions()} />);
    for (const c of welcomeComments) {
      expect(document.getElementById(`c-${c.id}`)).not.toBeNull();
    }
  });

  it("tombstoned comments render as [deleted] but keep their place in the tree", () => {
    const deleted: Comment[] = welcomeComments.map((c, i) => (i === 1 ? { ...c, deletedAt: new Date() } : c));
    render(() => <CommentTree comments={deleted} mode="tree" viewer={null} ctx={stubActions()} />);

    expect(screen.getByText("[deleted]")).toBeInTheDocument();
    // Still in the tree — its replies (the rest of the chain) still render.
    expect(renderedCommentIds()).toEqual(welcomeComments.map((c) => c.id));
  });
});
