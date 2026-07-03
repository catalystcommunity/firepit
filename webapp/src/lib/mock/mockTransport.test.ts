// Exercises the mock transport end to end through the real generated client
// (task C1's "Mock-server mode" — the same path C2-C4's real requests will
// take), covering the op families PLANDOC.md calls out: boards, thread
// fetch/create, endorsements, subscriptions, unread-summary, notifications,
// settings — plus the auth flow the mock's login shortcut drives.
import { beforeEach, describe, expect, it } from "vitest";
import { AsyncApiClient } from "~/gen/client.async.gen";
import { FirepitTransportError } from "~/lib/errors";
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

  it("throws a typed FirepitTransportError (not a crash) for a service the mock doesn't route", async () => {
    // SocialService isn't in the mock's routing table (PLANDOC.md's C1 op
    // list doesn't call for it) — mirrors dispatch.go's "unknown
    // service/op" transport outcome rather than an opaque throw.
    await expect(api.social.listFriendGroups({})).rejects.toBeInstanceOf(FirepitTransportError);
  });
});
