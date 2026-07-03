// Endorse/retract flow against the real mock transport (task C3 accept
// criterion: "endorsement flow"). Exercises the actual `FixtureStore`
// business logic (own-content block, idempotent list) rather than
// hand-mocked functions, and asserts the one rule this component must never
// break: `list-endorsements`' SERVER order is preserved — existing entries
// never move when a new one is optimistically added or a failed call is
// rolled back.
import { fireEvent, render, screen, waitFor } from "@solidjs/testing-library";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { AsyncApiClient } from "~/gen/client.async.gen";
import { createMockTransport } from "~/lib/mock/mockTransport";
import { FixtureStore } from "~/lib/mock/store";
import { MOCK_USER, posts } from "~/lib/mock/fixtures";
import Endorsements from "./Endorsements";

const { api } = vi.hoisted(() => ({ api: {} as unknown as AsyncApiClient }));

vi.mock("~/lib/api", () => ({ api }));

// Rebuilds the mocked `api` binding with a fresh store before every test —
// `vi.hoisted` gives us one shared object identity `~/lib/api`'s module
// captured at import time, so we mutate its methods in place per test
// rather than re-mocking the module (which vitest doesn't support mid-file).
function resetApi(): AsyncApiClient {
  const fresh = new AsyncApiClient(createMockTransport(new FixtureStore()));
  Object.assign(api, fresh);
  return api;
}

const welcomePost = posts.find((p) => p.title === "Welcome to Firepit");
if (!welcomePost) throw new Error("fixture missing the welcome post");

describe("Endorsements", () => {
  beforeEach(async () => {
    resetApi();
    await api.auth.beginLogin({ domain: MOCK_USER.linkkeysDomain });
  });

  it("shows the seeded endorser, endorses, and preserves server order", async () => {
    render(() => (
      <Endorsements
        targetType="post"
        targetId={welcomePost.id}
        authorId={welcomePost.authorId}
        isDeleted={false}
        viewer={MOCK_USER}
      />
    ));

    // Seed data (fixtures.ts): Carol (the post's author) already "endorsed"
    // it with a maintainer role badge.
    await waitFor(() => expect(screen.getByText("maintainer")).toBeInTheDocument());
    const initialOrder = [...document.querySelectorAll(".endorser")].map((el) => el.textContent);
    expect(initialOrder).toHaveLength(1);

    const endorseBtn = await screen.findByRole("button", { name: "Endorse" });
    fireEvent.click(endorseBtn);

    // Wait past the in-flight optimistic state (button reads "Retract
    // endorsement" immediately but stays disabled until the server
    // confirms) so the click below lands on an enabled button.
    await waitFor(() => expect(screen.getByRole("button", { name: "Retract endorsement" })).toBeEnabled());
    const afterEndorse = [...document.querySelectorAll(".endorser")].map((el) => el.textContent);
    expect(afterEndorse).toHaveLength(2);
    // The pre-existing (Carol) entry keeps its position; the new one is
    // appended — never re-sorted.
    expect(afterEndorse[0]).toBe(initialOrder[0]);

    // Retract: back to just the seeded entry, in the same order.
    fireEvent.click(screen.getByRole("button", { name: "Retract endorsement" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "Endorse" })).toBeEnabled());
    const afterRetract = [...document.querySelectorAll(".endorser")].map((el) => el.textContent);
    expect(afterRetract).toEqual(initialOrder);
  });

  it("rolls back the optimistic endorsement if the call fails", async () => {
    render(() => (
      <Endorsements
        targetType="post"
        targetId={welcomePost.id}
        authorId={welcomePost.authorId}
        isDeleted={false}
        viewer={MOCK_USER}
      />
    ));
    await waitFor(() => expect(screen.getByText("maintainer")).toBeInTheDocument());
    const before = [...document.querySelectorAll(".endorser")].map((el) => el.textContent);

    const spy = vi.spyOn(api.endorsement, "endorse").mockRejectedValueOnce(new Error("network blip"));
    fireEvent.click(screen.getByRole("button", { name: "Endorse" }));

    // Rolled back: still just the original entry, and the button reverts to "Endorse".
    await waitFor(() => expect(screen.getByText("network blip")).toBeInTheDocument());
    expect(screen.getByRole("button", { name: "Endorse" })).toBeInTheDocument();
    const after = [...document.querySelectorAll(".endorser")].map((el) => el.textContent);
    expect(after).toEqual(before);

    spy.mockRestore();
  });

  it("does not offer endorse to the content's own author", async () => {
    render(() => (
      <Endorsements
        targetType="post"
        targetId={welcomePost.id}
        authorId={MOCK_USER.id}
        isDeleted={false}
        viewer={MOCK_USER}
      />
    ));
    await waitFor(() => expect(screen.getByText("maintainer")).toBeInTheDocument());
    // authorId === viewer.id here (pretending the viewer authored it) — no
    // endorse/retract control should render at all.
    expect(screen.queryByRole("button", { name: "Endorse" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Retract endorsement" })).toBeNull();
  });
});
