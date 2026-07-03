// Component tests for the mention-grants section (task C4, PLANDOC.md §7's
// accept criterion "settings mutations round-trip ... grant add/revoke").
// Adding a grant now goes through SocialService.resolve-user first (a CSIL
// schema follow-up: the form accepts a handle, not a raw UserID).
import { render, screen, waitFor } from "@solidjs/testing-library";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { MentionGrantList, UserProfile } from "~/gen/types.gen";
import { FirepitServiceError, ServiceErrorCode } from "~/lib/errors";

const { listMentionGrants, grantMention, revokeMention, resolveUser } = vi.hoisted(() => ({
  listMentionGrants: vi.fn(),
  grantMention: vi.fn(),
  revokeMention: vi.fn(),
  resolveUser: vi.fn(),
}));

vi.mock("~/lib/api", () => ({
  api: { settings: { listMentionGrants, grantMention, revokeMention }, social: { resolveUser } },
}));

const { default: MentionGrantsSection } = await import("./MentionGrantsSection");

const SEEDED: MentionGrantList = { grants: [{ userId: "bob-id", createdAt: new Date("2026-01-01T00:00:00Z") }] };

const CAROL_PROFILE: UserProfile = {
  id: "carol-id",
  linkkeysDomain: "example.com",
  handle: "carol",
  displayName: "Carol Chen",
  kind: "human",
  roles: [],
  createdAt: new Date("2026-01-01T00:00:00Z"),
};

beforeEach(() => {
  listMentionGrants.mockReset().mockResolvedValue(SEEDED);
  grantMention.mockReset();
  revokeMention.mockReset();
  resolveUser.mockReset();
});

describe("MentionGrantsSection", () => {
  it("adding a grant by handle resolves it, calls grant-mention, and shows it in the list", async () => {
    resolveUser.mockResolvedValue(CAROL_PROFILE);
    grantMention.mockResolvedValue({});
    const user = userEvent.setup();

    render(() => <MentionGrantsSection />);
    await waitFor(() => expect(screen.getByText("bob-id")).toBeInTheDocument());

    await user.type(screen.getByLabelText("Handle"), "carol");
    await user.click(screen.getByRole("button", { name: "Grant" }));

    await waitFor(() => expect(resolveUser).toHaveBeenCalledWith("carol"));
    await waitFor(() => expect(grantMention).toHaveBeenCalledWith("carol-id"));
    await waitFor(() => expect(screen.getByText("carol-id")).toBeInTheDocument());
    expect(screen.getByLabelText("Handle")).toHaveValue("");
  });

  it("an unresolvable handle shows an inline NotFound error and never calls grant-mention", async () => {
    resolveUser.mockRejectedValue(new FirepitServiceError({ code: ServiceErrorCode.NotFound, message: 'no user with that handle' }));
    const user = userEvent.setup();

    render(() => <MentionGrantsSection />);
    await waitFor(() => expect(screen.getByText("bob-id")).toBeInTheDocument());

    await user.type(screen.getByLabelText("Handle"), "nope");
    await user.click(screen.getByRole("button", { name: "Grant" }));

    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent('No user found with handle "nope"'));
    expect(grantMention).not.toHaveBeenCalled();
    expect(screen.queryByText("nope")).not.toBeInTheDocument();
  });

  it("a failed grant (after a successful resolve) rolls back the optimistic row and shows the ServiceError", async () => {
    resolveUser.mockResolvedValue(CAROL_PROFILE);
    grantMention.mockRejectedValue(new FirepitServiceError({ code: ServiceErrorCode.Conflict, message: "already granted" }));
    const user = userEvent.setup();

    render(() => <MentionGrantsSection />);
    await waitFor(() => expect(screen.getByText("bob-id")).toBeInTheDocument());

    await user.type(screen.getByLabelText("Handle"), "carol");
    await user.click(screen.getByRole("button", { name: "Grant" }));

    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent("already granted"));
    expect(screen.queryByText("carol-id")).not.toBeInTheDocument();
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
