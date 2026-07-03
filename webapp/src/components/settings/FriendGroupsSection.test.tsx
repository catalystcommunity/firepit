// Component tests for the friend-groups section (task C4, PLANDOC.md §7's
// accept criterion "settings mutations round-trip ... group create/add-member").
// Adding a member now goes through SocialService.resolve-user first (a CSIL
// schema follow-up: the form accepts a handle, not a raw UserID).
import { render, screen, waitFor, within } from "@solidjs/testing-library";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { FriendGroup, FriendGroupList, UserProfile } from "~/gen/types.gen";
import { FirepitServiceError, ServiceErrorCode } from "~/lib/errors";

const { listFriendGroups, createFriendGroup, deleteFriendGroup, addFriend, removeFriend, resolveUser } = vi.hoisted(() => ({
  listFriendGroups: vi.fn(),
  createFriendGroup: vi.fn(),
  deleteFriendGroup: vi.fn(),
  addFriend: vi.fn(),
  removeFriend: vi.fn(),
  resolveUser: vi.fn(),
}));

vi.mock("~/lib/api", () => ({
  api: { social: { listFriendGroups, createFriendGroup, deleteFriendGroup, addFriend, removeFriend, resolveUser } },
}));

const { default: FriendGroupsSection } = await import("./FriendGroupsSection");

const CORE_GROUP: FriendGroup = { id: "group-1", name: "Core reviewers", members: ["carol-id"], createdAt: new Date("2026-01-01T00:00:00Z") };
const SEEDED: FriendGroupList = { groups: [CORE_GROUP] };

const DAVE_PROFILE: UserProfile = {
  id: "dave-id",
  linkkeysDomain: "example.com",
  handle: "dave",
  displayName: "Dave Diaz",
  kind: "human",
  roles: [],
  createdAt: new Date("2026-01-01T00:00:00Z"),
};

beforeEach(() => {
  listFriendGroups.mockReset().mockResolvedValue(SEEDED);
  createFriendGroup.mockReset();
  deleteFriendGroup.mockReset();
  addFriend.mockReset();
  removeFriend.mockReset();
  resolveUser.mockReset();
});

describe("FriendGroupsSection", () => {
  it("creating a group calls create-friend-group and renders it", async () => {
    createFriendGroup.mockResolvedValue({ id: "group-2", name: "Weekend crew", members: [], createdAt: new Date() });
    const user = userEvent.setup();

    render(() => <FriendGroupsSection />);
    await waitFor(() => expect(screen.getByText("Core reviewers")).toBeInTheDocument());

    await user.type(screen.getByLabelText("New group name"), "Weekend crew");
    await user.click(screen.getByRole("button", { name: "Create group" }));

    await waitFor(() => expect(createFriendGroup).toHaveBeenCalledWith({ name: "Weekend crew" }));
    await waitFor(() => expect(screen.getByText("Weekend crew")).toBeInTheDocument());
  });

  it("adding a member by handle resolves it, calls add-friend, and shows the new member under that group", async () => {
    resolveUser.mockResolvedValue(DAVE_PROFILE);
    addFriend.mockResolvedValue({});
    const user = userEvent.setup();

    render(() => <FriendGroupsSection />);
    await waitFor(() => expect(screen.getByText("Core reviewers")).toBeInTheDocument());

    const groupCard = () => screen.getByText("Core reviewers").closest("li") as HTMLElement;
    await user.type(within(groupCard()).getByLabelText("Add member (handle)"), "dave");
    await user.click(within(groupCard()).getByRole("button", { name: "Add" }));

    await waitFor(() => expect(resolveUser).toHaveBeenCalledWith("dave"));
    await waitFor(() => expect(addFriend).toHaveBeenCalledWith({ groupId: "group-1", userId: "dave-id" }));
    // Re-query rather than reusing the earlier `<li>` node: adding a member
    // replaces that group's array entry with a new object, and Solid's
    // `<For>` keys by item identity — the whole subtree for this group is
    // torn down and rebuilt, so a captured element reference goes stale.
    await waitFor(() => expect(within(groupCard()).getByText("dave-id")).toBeInTheDocument());
  });

  it("an unresolvable handle shows an inline NotFound error and never calls add-friend", async () => {
    resolveUser.mockRejectedValue(new FirepitServiceError({ code: ServiceErrorCode.NotFound, message: "no user with that handle" }));
    const user = userEvent.setup();

    render(() => <FriendGroupsSection />);
    await waitFor(() => expect(screen.getByText("Core reviewers")).toBeInTheDocument());

    const groupCard = () => screen.getByText("Core reviewers").closest("li") as HTMLElement;
    await user.type(within(groupCard()).getByLabelText("Add member (handle)"), "nope");
    await user.click(within(groupCard()).getByRole("button", { name: "Add" }));

    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent('No user found with handle "nope"'));
    expect(addFriend).not.toHaveBeenCalled();
  });

  it("deleting a group calls delete-friend-group and removes it", async () => {
    deleteFriendGroup.mockResolvedValue({});
    const user = userEvent.setup();

    render(() => <FriendGroupsSection />);
    await waitFor(() => expect(screen.getByText("Core reviewers")).toBeInTheDocument());

    await user.click(screen.getByRole("button", { name: "Delete group" }));

    await waitFor(() => expect(deleteFriendGroup).toHaveBeenCalledWith("group-1"));
    await waitFor(() => expect(screen.queryByText("Core reviewers")).not.toBeInTheDocument());
  });
});
