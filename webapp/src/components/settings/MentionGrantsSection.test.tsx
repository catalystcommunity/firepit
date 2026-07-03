// Component tests for the mention-grants section (task C4, PLANDOC.md §7's
// accept criterion "settings mutations round-trip ... grant add/revoke").
import { render, screen, waitFor } from "@solidjs/testing-library";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { MentionGrantList } from "~/gen/types.gen";
import { FirepitServiceError, ServiceErrorCode } from "~/lib/errors";

const { listMentionGrants, grantMention, revokeMention } = vi.hoisted(() => ({
  listMentionGrants: vi.fn(),
  grantMention: vi.fn(),
  revokeMention: vi.fn(),
}));

vi.mock("~/lib/api", () => ({
  api: { settings: { listMentionGrants, grantMention, revokeMention } },
}));

const { default: MentionGrantsSection } = await import("./MentionGrantsSection");

const SEEDED: MentionGrantList = { grants: [{ userId: "bob-id", createdAt: new Date("2026-01-01T00:00:00Z") }] };

beforeEach(() => {
  listMentionGrants.mockReset().mockResolvedValue(SEEDED);
  grantMention.mockReset();
  revokeMention.mockReset();
});

describe("MentionGrantsSection", () => {
  it("adding a grant by user id calls grant-mention and shows it in the list", async () => {
    grantMention.mockResolvedValue({});
    const user = userEvent.setup();

    render(() => <MentionGrantsSection />);
    await waitFor(() => expect(screen.getByText("bob-id")).toBeInTheDocument());

    await user.type(screen.getByLabelText("User ID"), "carol-id");
    await user.click(screen.getByRole("button", { name: "Grant" }));

    await waitFor(() => expect(grantMention).toHaveBeenCalledWith("carol-id"));
    await waitFor(() => expect(screen.getByText("carol-id")).toBeInTheDocument());
    expect(screen.getByLabelText("User ID")).toHaveValue("");
  });

  it("a failed grant rolls back the optimistic row and shows the ServiceError", async () => {
    grantMention.mockRejectedValue(new FirepitServiceError({ code: ServiceErrorCode.NotFound, message: "no user with id \"nope\"" }));
    const user = userEvent.setup();

    render(() => <MentionGrantsSection />);
    await waitFor(() => expect(screen.getByText("bob-id")).toBeInTheDocument());

    await user.type(screen.getByLabelText("User ID"), "nope");
    await user.click(screen.getByRole("button", { name: "Grant" }));

    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent('no user with id "nope"'));
    expect(screen.queryByText("nope")).not.toBeInTheDocument();
  });

  it("revoking a grant calls revoke-mention and removes the row", async () => {
    revokeMention.mockResolvedValue({});
    const user = userEvent.setup();

    render(() => <MentionGrantsSection />);
    await waitFor(() => expect(screen.getByText("bob-id")).toBeInTheDocument());

    await user.click(screen.getByRole("button", { name: "Revoke" }));

    await waitFor(() => expect(revokeMention).toHaveBeenCalledWith("bob-id"));
    await waitFor(() => expect(screen.queryByText("bob-id")).not.toBeInTheDocument());
  });
});
