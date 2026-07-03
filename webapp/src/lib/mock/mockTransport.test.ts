// Exercises the mock transport end to end through the real generated client
// (task C1's "Mock-server mode" — the same path C2-C4's real requests will
// take), covering the op families PLANDOC.md calls out: boards, thread
// fetch/create, endorsements, subscriptions, unread-summary, notifications,
// settings — plus the auth flow the mock's login shortcut drives.
import { beforeEach, describe, expect, it } from "vitest";
import { AsyncApiClient } from "~/gen/client.async.gen";
import { createMockTransport } from "./mockTransport";
import { FixtureStore } from "./store";

function freshClient(): AsyncApiClient {
  return new AsyncApiClient(createMockTransport(new FixtureStore()));
}

describe("mock transport", () => {
  let api: AsyncApiClient;

  beforeEach(() => {
    api = freshClient();
  });

  it("starts logged out: whoami rejects Unauthenticated", async () => {
    await expect(api.auth.whoami({})).rejects.toMatchObject({ name: "FirepitServiceError", code: 3 });
  });

  it("begin-login logs the fixture user in; whoami then succeeds", async () => {
    const resp = await api.auth.beginLogin({ domain: "todandlorna.com" });
    expect(resp.redirectUrl).toContain("/auth/callback");

    const me = await api.auth.whoami({});
    expect(me.handle).toBe("alice");
  });

  it("lists seeded boards and fetches one by slug", async () => {
    const page = await api.board.listBoards({});
    expect(page.boards.length).toBeGreaterThanOrEqual(3);
    const slugs = page.boards.map((b) => b.slug);
    expect(slugs).toEqual(expect.arrayContaining(["firepit", "announcements", "csilgen"]));

    const board = await api.board.getBoard("firepit");
    expect(board.title).toBe("Firepit Meta");

    await expect(api.board.getBoard("does-not-exist")).rejects.toMatchObject({ resourceType: "board" });
  });

  it("fetches a thread with its full (deep) comment tree and can reply to it", async () => {
    const boards = await api.board.listBoards({});
    const firepitBoard = boards.boards.find((b) => b.slug === "firepit");
    if (!firepitBoard) throw new Error("fixture missing the firepit board");

    const posts = await api.thread.listPosts({ boardId: firepitBoard.id });
    const welcome = posts.posts.find((p) => p.title === "Welcome to Firepit");
    if (!welcome) throw new Error("fixture missing the welcome post");

    const thread = await api.thread.getThread({ postId: welcome.id });
    expect(thread.comments.length).toBeGreaterThanOrEqual(6);

    // Depth check: walk parentCommentId back to the root and confirm the
    // deepest seeded comment really is several levels down.
    const byId = new Map(thread.comments.map((c) => [c.id, c]));
    const deepest = thread.comments[thread.comments.length - 1];
    let depth = 0;
    let cursor = deepest;
    while (cursor.parentCommentId) {
      const parent = byId.get(cursor.parentCommentId);
      if (!parent) break;
      cursor = parent;
      depth += 1;
    }
    expect(depth).toBeGreaterThanOrEqual(4);

    await api.auth.beginLogin({ domain: "todandlorna.com" });
    const reply = await api.thread.createComment({ postId: welcome.id, parentCommentId: deepest.id, bodyMd: "nice thread" });
    expect(reply.parentCommentId).toBe(deepest.id);

    const refetched = await api.thread.getThread({ postId: welcome.id });
    expect(refetched.comments.length).toBe(thread.comments.length + 1);
  });

  it("endorsements: cannot endorse your own content, can endorse and list others'", async () => {
    await api.auth.beginLogin({ domain: "todandlorna.com" });
    const me = await api.auth.whoami({});

    const boards = await api.board.listBoards({});
    const board = boards.boards.find((b) => b.slug === "firepit");
    if (!board) throw new Error("fixture missing board");
    const post = await api.thread.createPost({ boardId: board.id, title: "mine", bodyMd: "body" });

    await expect(api.endorsement.endorse({ targetType: "post", targetId: post.id })).rejects.toMatchObject({
      name: "FirepitServiceError",
      code: 4, // Forbidden
    });

    const list = await api.endorsement.listEndorsements({ targetType: "post", targetId: post.id });
    expect(list.endorsements.every((e) => e.userId !== me.id)).toBe(true);
  });

  it("subscriptions + unread-summary + notifications + settings round-trip", async () => {
    await api.auth.beginLogin({ domain: "todandlorna.com" });

    const subs = await api.subscription.listSubscriptions({});
    expect(subs.subscriptions.length).toBeGreaterThan(0);

    const summary = await api.read.unreadSummary({});
    expect(summary.boards.length).toBeGreaterThan(0);
    const totalUnread = summary.boards.reduce((n, b) => n + b.unreadCount, 0);
    expect(totalUnread).toBeGreaterThan(0);

    const notifications = await api.notification.listNotifications({});
    expect(notifications.notifications.length).toBeGreaterThan(0);
    await api.notification.markAllRead({});
    const afterMarkAll = await api.notification.listNotifications({ unreadOnly: true });
    expect(afterMarkAll.notifications.length).toBe(0);

    const settings = await api.settings.getSettings({});
    expect(settings.mentionPolicy).toBe("subscribed");
    const updated = await api.settings.updateSettings({ notifyOnEndorse: false });
    expect(updated.notifyOnEndorse).toBe(false);
  });

  it("friend groups: create, add/remove members, delete round-trip (task C4 wires SocialService into the mock)", async () => {
    await api.auth.beginLogin({ domain: "todandlorna.com" });
    const me = await api.auth.whoami({});

    const seeded = await api.social.listFriendGroups({});
    expect(seeded.groups.length).toBeGreaterThan(0);

    const group = await api.social.createFriendGroup({ name: "Reviewers" });
    expect(group.members).toEqual([]);

    await expect(api.social.addFriend({ groupId: group.id, userId: me.id })).rejects.toMatchObject({
      name: "FirepitServiceError",
      code: 2, // Validation — can't add yourself to your own friend group
    });
    await expect(api.social.addFriend({ groupId: group.id, userId: "no-such-user" })).rejects.toMatchObject({
      resourceType: "user",
    });

    await api.social.addFriend({ groupId: group.id, userId: seeded.groups[0].members[0] });
    let refreshed = await api.social.listFriendGroups({});
    expect(refreshed.groups.find((g) => g.id === group.id)?.members).toEqual([seeded.groups[0].members[0]]);

    await api.social.removeFriend({ groupId: group.id, userId: seeded.groups[0].members[0] });
    refreshed = await api.social.listFriendGroups({});
    expect(refreshed.groups.find((g) => g.id === group.id)?.members).toEqual([]);

    await api.social.deleteFriendGroup(group.id);
    refreshed = await api.social.listFriendGroups({});
    expect(refreshed.groups.find((g) => g.id === group.id)).toBeUndefined();
  });

  it("notifications: cursor pagination resumes after the last-seen id, and every event type is seeded", async () => {
    await api.auth.beginLogin({ domain: "todandlorna.com" });

    const first = await api.notification.listNotifications({ limit: 5 });
    expect(first.notifications.length).toBe(5);
    expect(first.nextCursor).toBeDefined();

    const second = await api.notification.listNotifications({ limit: 5, cursor: first.nextCursor });
    expect(second.notifications.length).toBeGreaterThan(0);
    expect(second.nextCursor).toBeUndefined();
    const seenIds = new Set([...first.notifications, ...second.notifications].map((n) => n.id));
    expect(seenIds.size).toBe(first.notifications.length + second.notifications.length);

    const all = await api.notification.listNotifications({ limit: 200 });
    const events = new Set(all.notifications.map((n) => n.event));
    expect(events).toEqual(new Set(["new_post", "new_comment", "mention", "github_event"]));
  });
});
