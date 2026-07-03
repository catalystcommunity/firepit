// Revision history panel (task C3 accept criterion: "revision history
// view"). Against the real mock transport so `list-revisions` runs the same
// `FixtureStore` logic the app does.
import { fireEvent, render, screen, waitFor } from "@solidjs/testing-library";
import { describe, expect, it, vi } from "vitest";
import { AsyncApiClient } from "~/gen/client.async.gen";
import { createMockTransport } from "~/lib/mock/mockTransport";
import { FixtureStore } from "~/lib/mock/store";
import { comments, posts } from "~/lib/mock/fixtures";
import RevisionHistory from "./RevisionHistory";

const { api } = vi.hoisted(() => ({ api: {} as unknown as AsyncApiClient }));
vi.mock("~/lib/api", () => ({ api }));

const editedComment = comments.find((c) => c.editedAt !== undefined);
if (!editedComment) throw new Error("fixture has no edited comment to test revision history against");

describe("RevisionHistory", () => {
  it("is collapsed by default and lazily loads on expand", async () => {
    Object.assign(api, new AsyncApiClient(createMockTransport(new FixtureStore())));

    render(() => <RevisionHistory targetType="comment" targetId={editedComment.id} />);

    expect(screen.queryByText(/readable/i)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "View edit history" }));

    await waitFor(() => expect(screen.getByText(/readable/i)).toBeInTheDocument());
    expect(screen.getByRole("button", { name: "Hide edit history" })).toBeInTheDocument();
  });

  it("shows 'no prior revisions' for an item with no edit history", async () => {
    Object.assign(api, new AsyncApiClient(createMockTransport(new FixtureStore())));
    const unedited = posts.find((p) => p.editedAt === undefined);
    if (!unedited) throw new Error("fixture has no never-edited post to test against");

    render(() => <RevisionHistory targetType="post" targetId={unedited.id} />);
    fireEvent.click(screen.getByRole("button", { name: "View edit history" }));

    await waitFor(() => expect(screen.getByText("No prior revisions.")).toBeInTheDocument());
  });
});
