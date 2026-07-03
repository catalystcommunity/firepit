// Mock CSIL business logic (task C1's "Mock-server mode" — PLANDOC.md §7).
// A `FixtureStore` holds one in-memory snapshot (see fixtures.ts) plus the
// bit of mutable state a mock login/read/notification/subscription flow
// needs, and implements roughly the same operations
// `api/internal/csilservices` does — just enough behavior (authz checks,
// not-found/conflict errors, id generation) that C2-C4 exercise real
// success/failure paths without a backend. `mockTransport.ts` is the thin
// (service, op) <-> codec wiring on top of this; this file has no CBOR or
// wire-format awareness at all.
import type {
  AddFriendRequest,
  Board,
  BoardPage,
  Comment,
  CreateBoardRequest,
  CreateCommentRequest,
  CreateFriendGroupRequest,
  CreatePostRequest,
  Empty,
  Endorsement,
  EndorsementList,
  FriendGroup,
  FriendGroupList,
  ListNotificationsRequest,
  MentionGrantList,
  NotificationPage,
  Post,
  PostPage,
  RemoveFriendRequest,
  RevisionList,
  Subscription,
  SubscriptionList,
  TargetRef,
  TargetType,
  Thread,
  UnreadSummary,
  UpdateBoardRequest,
  UpdateSettingsRequest,
  UserProfile,
  UserSettings,
} from "~/gen/types.gen";
import { FirepitServiceError, ServiceErrorCode } from "~/lib/errors";
import { createSeed, MOCK_USER_ID, OTHER_USER_IDS, type Seed } from "./fixtures";

// The mock's entire "does this user exist" universe (task C4): the caller
// plus the three other named fixture users. Real GrantMention/AddFriend
// (api/internal/csilservices/settings.go, social.go) reject an unknown
// target with NotFound the same way — mirrored here so a settings UI
// exercising a bad id sees the same class of ServiceError against the mock
// it would against the real API, not a silent no-op.
const KNOWN_USER_IDS = new Set<string>([MOCK_USER_ID, ...OTHER_USER_IDS]);

function serviceError(code: number, message: string, extra: { field?: string; resourceType?: string } = {}) {
  return new FirepitServiceError({ code, message, ...extra });
}

const notFound = (resourceType: string, message: string) =>
  serviceError(ServiceErrorCode.NotFound, message, { resourceType });
const forbidden = (message: string) => serviceError(ServiceErrorCode.Forbidden, message);
const conflict = (message: string) => serviceError(ServiceErrorCode.Conflict, message);
const unauthenticated = (message: string) => serviceError(ServiceErrorCode.Unauthenticated, message);
const validation = (field: string, message: string) => serviceError(ServiceErrorCode.Validation, message, { field });

export interface FixtureStoreOptions {
  /**
   * Persist the mock "logged in" flag to `sessionStorage` so it survives a
   * real full-page navigation. `session.login()` does a real
   * `window.location.href` assignment (matching the real login flow's 302
   * to an IDP and back) — in a real browser that reloads the page and
   * throws away every in-memory JS object, this `FixtureStore` included, so
   * without this the mock would "log in" and then immediately look
   * logged-out again on arrival at `/auth/callback`. Off by default so
   * tests constructing their own `FixtureStore` get a clean, storage-free
   * instance every time; `src/lib/mock/mockTransport.ts`'s zero-arg
   * `createMockTransport()` (what `src/lib/api.ts`'s singleton uses) turns
   * it on.
   */
  persistLogin?: boolean;
}

const SESSION_STORAGE_KEY = "firepit-mock-logged-in";

export class FixtureStore {
  private readonly seed: Seed;
  private readonly persistLogin: boolean;
  private loggedIn: boolean;
  private genCounter = 0;
  private readonly readPostIds: Set<string>;

  constructor(seed: Seed = createSeed(), opts: FixtureStoreOptions = {}) {
    this.seed = seed;
    this.persistLogin = opts.persistLogin ?? false;
    this.loggedIn = this.persistLogin && this.readPersistedLogin();
    // Everything is unread by default except the one post on a board with no
    // subscription — gives a non-trivial, but not overwhelming, unread
    // summary out of the box (see unreadSummary below).
    this.readPostIds = new Set(this.seed.posts.filter((p) => p.title.startsWith("Why kebab-case")).map((p) => p.id));
  }

  private readPersistedLogin(): boolean {
    if (typeof sessionStorage === "undefined") return false;
    return sessionStorage.getItem(SESSION_STORAGE_KEY) === "1";
  }

  private writePersistedLogin(loggedIn: boolean): void {
    if (!this.persistLogin || typeof sessionStorage === "undefined") return;
    if (loggedIn) sessionStorage.setItem(SESSION_STORAGE_KEY, "1");
    else sessionStorage.removeItem(SESSION_STORAGE_KEY);
  }

  private nextId(label: string): string {
    this.genCounter += 1;
    return `01FPMOCKGEN${label.toUpperCase().slice(0, 6).padEnd(6, "X")}${String(this.genCounter).padStart(6, "0")}`;
  }

  private requireAuth(): UserProfile {
    if (!this.loggedIn) throw unauthenticated("no active session");
    return this.seed.user;
  }

  // ---------------------------------------------------------------- auth --

  beginLogin(domain: string): { redirectUrl: string } {
    if (domain.length === 0) throw validation("domain", "domain must not be empty");
    // Real backend: begin-login calls the linkkeys RP sidecar and 302s the
    // browser to that domain's IDP; there is no IDP to bounce through here,
    // so the mock takes the pragmatic shortcut of logging the fixture user
    // in immediately (as if that whole round-trip had just succeeded) and
    // pointing `redirectUrl` at our own `/auth/callback` — the SPA still
    // exercises the real landing-page + re-whoami flow end to end, just
    // without leaving the app (see PLANDOC.md C1's accept criterion "login
    // flow against mock").
    this.loggedIn = true;
    this.writePersistedLogin(true);
    return { redirectUrl: `/auth/callback?mock_domain=${encodeURIComponent(domain)}` };
  }

  logout(): void {
    this.loggedIn = false;
    this.writePersistedLogin(false);
  }

  whoami(): UserProfile {
    return structuredClone(this.requireAuth());
  }

  // -------------------------------------------------------------- boards --

  listBoards(): BoardPage {
    return { boards: structuredClone(this.seed.boards) };
  }

  getBoard(slug: string): Board {
    const board = this.seed.boards.find((b) => b.slug === slug);
    if (!board) throw notFound("board", `no board with slug "${slug}"`);
    return structuredClone(board);
  }

  createBoard(req: CreateBoardRequest): Board {
    const caller = this.requireAuth();
    if (this.seed.boards.some((b) => b.slug === req.slug)) {
      throw conflict(`a board with slug "${req.slug}" already exists`);
    }
    const board: Board = {
      id: this.nextId("board"),
      slug: req.slug,
      title: req.title,
      description: req.description,
      kind: req.kind,
      createdBy: caller.id,
      createdAt: new Date(),
    };
    this.seed.boards.push(board);
    return structuredClone(board);
  }

  updateBoard(req: UpdateBoardRequest): Board {
    this.requireAuth();
    const board = this.seed.boards.find((b) => b.id === req.id);
    if (!board) throw notFound("board", `no board with id "${req.id}"`);
    if (req.title !== undefined) board.title = req.title;
    if (req.description !== undefined) board.description = req.description;
    return structuredClone(board);
  }

  archiveBoard(id: string): Empty {
    this.requireAuth();
    const board = this.seed.boards.find((b) => b.id === id);
    if (!board) throw notFound("board", `no board with id "${id}"`);
    board.archivedAt = new Date();
    return {};
  }

  // -------------------------------------------------------------- threads --

  listPosts(boardId: string): PostPage {
    const boardPosts = this.seed.posts
      .filter((p) => p.boardId === boardId)
      .sort((a, b) => b.lastActivityAt.getTime() - a.lastActivityAt.getTime());
    return { posts: structuredClone(boardPosts) };
  }

  getThread(postId: string): Thread {
    const post = this.seed.posts.find((p) => p.id === postId);
    if (!post) throw notFound("post", `no post with id "${postId}"`);
    const threadComments = this.seed.comments
      .filter((c) => c.postId === postId)
      .sort((a, b) => a.createdAt.getTime() - b.createdAt.getTime());
    return { post: structuredClone(post), comments: structuredClone(threadComments) };
  }

  createPost(req: CreatePostRequest): Post {
    const caller = this.requireAuth();
    if (!this.seed.boards.some((b) => b.id === req.boardId)) {
      throw notFound("board", `no board with id "${req.boardId}"`);
    }
    const now = new Date();
    const post: Post = {
      id: this.nextId("post"),
      boardId: req.boardId,
      authorId: caller.id,
      title: req.title,
      bodyMd: req.bodyMd,
      origin: "user",
      commentCount: 0,
      lastActivityAt: now,
      createdAt: now,
    };
    this.seed.posts.push(post);
    this.readPostIds.add(post.id); // the author has, trivially, "read" their own new post
    return structuredClone(post);
  }

  createComment(req: CreateCommentRequest): Comment {
    const caller = this.requireAuth();
    const post = this.seed.posts.find((p) => p.id === req.postId);
    if (!post) throw notFound("post", `no post with id "${req.postId}"`);
    if (req.parentCommentId && !this.seed.comments.some((c) => c.id === req.parentCommentId)) {
      throw notFound("comment", `no comment with id "${req.parentCommentId}"`);
    }
    const now = new Date();
    const comment: Comment = {
      id: this.nextId("comment"),
      postId: req.postId,
      parentCommentId: req.parentCommentId,
      authorId: caller.id,
      bodyMd: req.bodyMd,
      origin: "user",
      createdAt: now,
    };
    this.seed.comments.push(comment);
    post.commentCount += 1;
    post.lastActivityAt = now;
    return structuredClone(comment);
  }

  editPost(id: string, title: string, bodyMd: string): Post {
    this.requireAuth();
    const post = this.seed.posts.find((p) => p.id === id);
    if (!post) throw notFound("post", `no post with id "${id}"`);
    this.seed.revisions.push({
      id: this.nextId("rev"),
      targetType: "post",
      targetId: post.id,
      editorId: post.authorId,
      prevTitle: post.title,
      prevBodyMd: post.bodyMd,
      createdAt: new Date(),
    });
    post.title = title;
    post.bodyMd = bodyMd;
    post.editedAt = new Date();
    return structuredClone(post);
  }

  editComment(id: string, bodyMd: string): Comment {
    this.requireAuth();
    const comment = this.seed.comments.find((c) => c.id === id);
    if (!comment) throw notFound("comment", `no comment with id "${id}"`);
    this.seed.revisions.push({
      id: this.nextId("rev"),
      targetType: "comment",
      targetId: comment.id,
      editorId: comment.authorId,
      prevBodyMd: comment.bodyMd,
      createdAt: new Date(),
    });
    comment.bodyMd = bodyMd;
    comment.editedAt = new Date();
    return structuredClone(comment);
  }

  listRevisions(target: TargetRef): RevisionList {
    const revisions = this.seed.revisions
      .filter((r) => r.targetType === target.targetType && r.targetId === target.targetId)
      .sort((a, b) => b.createdAt.getTime() - a.createdAt.getTime());
    return { revisions: structuredClone(revisions) };
  }

  deletePost(id: string): Empty {
    this.requireAuth();
    const post = this.seed.posts.find((p) => p.id === id);
    if (!post) throw notFound("post", `no post with id "${id}"`);
    post.deletedAt = new Date();
    return {};
  }

  deleteComment(id: string): Empty {
    this.requireAuth();
    const comment = this.seed.comments.find((c) => c.id === id);
    if (!comment) throw notFound("comment", `no comment with id "${id}"`);
    comment.deletedAt = new Date();
    return {};
  }

  // --------------------------------------------------------- endorsements --

  private findTarget(target: TargetRef): { deletedAt?: Date; authorId: string } {
    if (target.targetType === "post") {
      const post = this.seed.posts.find((p) => p.id === target.targetId);
      if (!post) throw notFound("post", `no post with id "${target.targetId}"`);
      return post;
    }
    if (target.targetType === "comment") {
      const comment = this.seed.comments.find((c) => c.id === target.targetId);
      if (!comment) throw notFound("comment", `no comment with id "${target.targetId}"`);
      return comment;
    }
    throw validation("targetType", `cannot endorse a target of type "${target.targetType}"`);
  }

  endorse(target: TargetRef): Endorsement {
    const caller = this.requireAuth();
    const content = this.findTarget(target);
    if (content.deletedAt) throw notFound(target.targetType, "cannot endorse deleted content");
    if (content.authorId === caller.id) throw forbidden("cannot endorse your own content");
    if (
      this.seed.endorsements.some(
        (e) => e.userId === caller.id && e.targetType === target.targetType && e.targetId === target.targetId,
      )
    ) {
      throw conflict("already endorsed");
    }
    const endorsement: Endorsement = {
      id: this.nextId("endorse"),
      userId: caller.id,
      targetType: target.targetType,
      targetId: target.targetId,
      createdAt: new Date(),
    };
    this.seed.endorsements.push(endorsement);
    return structuredClone(endorsement);
  }

  retract(target: TargetRef): Empty {
    const caller = this.requireAuth();
    const idx = this.seed.endorsements.findIndex(
      (e) => e.userId === caller.id && e.targetType === target.targetType && e.targetId === target.targetId,
    );
    if (idx === -1) throw notFound("endorsement", "no endorsement to retract");
    this.seed.endorsements.splice(idx, 1);
    return {};
  }

  listEndorsements(target: TargetRef): EndorsementList {
    // Real ordering (PLANDOC.md §4): viewer's friends first, then
    // reputation. The mock has no friend-group/reputation machinery, so it
    // returns fixture (= insertion) order — good enough to render, not a
    // claim about ordering semantics.
    const list = this.seed.endorsements.filter(
      (e) => e.targetType === target.targetType && e.targetId === target.targetId,
    );
    return { endorsements: structuredClone(list) };
  }

  // -------------------------------------------------------- subscriptions --

  subscribe(target: TargetRef): Subscription {
    this.requireAuth();
    const existing = this.seed.subscriptions.find(
      (s) => s.targetType === target.targetType && s.targetId === target.targetId,
    );
    if (existing) return structuredClone(existing);
    const sub: Subscription = {
      id: this.nextId("sub"),
      targetType: target.targetType,
      targetId: target.targetId,
      muted: false,
      createdAt: new Date(),
    };
    this.seed.subscriptions.push(sub);
    return structuredClone(sub);
  }

  unsubscribe(target: TargetRef): Empty {
    this.requireAuth();
    const idx = this.seed.subscriptions.findIndex(
      (s) => s.targetType === target.targetType && s.targetId === target.targetId,
    );
    if (idx === -1) throw notFound("subscription", "no subscription to remove");
    this.seed.subscriptions.splice(idx, 1);
    return {};
  }

  setMuted(target: TargetRef, muted: boolean): Subscription {
    this.requireAuth();
    const sub = this.seed.subscriptions.find(
      (s) => s.targetType === target.targetType && s.targetId === target.targetId,
    );
    if (!sub) throw notFound("subscription", "no subscription to mute/unmute");
    sub.muted = muted;
    return structuredClone(sub);
  }

  listSubscriptions(): SubscriptionList {
    this.requireAuth();
    return { subscriptions: structuredClone(this.seed.subscriptions) };
  }

  // --------------------------------------------------------------- reads --

  private targetPostId(type: TargetType, id: string): string | undefined {
    if (type === "post") return id;
    if (type === "comment") return this.seed.comments.find((c) => c.id === id)?.postId;
    return undefined;
  }

  markRead(target: TargetRef): Empty {
    this.requireAuth();
    const postId = this.targetPostId(target.targetType, target.targetId);
    if (postId) this.readPostIds.add(postId);
    return {};
  }

  markUnread(target: TargetRef): Empty {
    this.requireAuth();
    const postId = this.targetPostId(target.targetType, target.targetId);
    if (postId) this.readPostIds.delete(postId);
    return {};
  }

  unreadSummary(): UnreadSummary {
    this.requireAuth();
    const byBoard = new Map<string, string[]>();
    for (const post of this.seed.posts) {
      if (this.readPostIds.has(post.id)) continue;
      const ids = byBoard.get(post.boardId) ?? [];
      ids.push(post.id);
      byBoard.set(post.boardId, ids);
    }
    const boards = [...byBoard.entries()].map(([boardId, unreadPostIds]) => ({
      boardId,
      unreadCount: unreadPostIds.length,
      unreadPostIds,
    }));
    return { boards };
  }

  // -------------------------------------------------------- notifications --

  listNotifications(req: ListNotificationsRequest): NotificationPage {
    this.requireAuth();
    let list = [...this.seed.notifications].sort(
      (a, b) => b.createdAt.getTime() - a.createdAt.getTime() || b.id.localeCompare(a.id),
    );
    if (req.unreadOnly) list = list.filter((n) => !n.readAt);
    if (req.cursor) {
      // Opaque cursor = the last-seen notification's id (see PageCursor's
      // doc comment: clients only ever pass this back verbatim, never
      // parse it) — resume immediately after it in the same sorted order.
      const idx = list.findIndex((n) => n.id === req.cursor);
      list = idx === -1 ? [] : list.slice(idx + 1);
    }
    const limit = req.limit ?? 50;
    const page = list.slice(0, limit);
    const nextCursor = list.length > limit ? page[page.length - 1]?.id : undefined;
    return { notifications: structuredClone(page), nextCursor };
  }

  markNotificationRead(ids: readonly string[]): Empty {
    this.requireAuth();
    const now = new Date();
    for (const n of this.seed.notifications) {
      if (ids.includes(n.id)) n.readAt = n.readAt ?? now;
    }
    return {};
  }

  markAllRead(): Empty {
    this.requireAuth();
    const now = new Date();
    for (const n of this.seed.notifications) n.readAt = n.readAt ?? now;
    return {};
  }

  // ------------------------------------------------------------ settings --

  getSettings(): UserSettings {
    this.requireAuth();
    return structuredClone(this.seed.settings);
  }

  updateSettings(req: UpdateSettingsRequest): UserSettings {
    this.requireAuth();
    if (req.mentionPolicy !== undefined) this.seed.settings.mentionPolicy = req.mentionPolicy;
    if (req.notifyOnEndorse !== undefined) this.seed.settings.notifyOnEndorse = req.notifyOnEndorse;
    this.seed.settings.updatedAt = new Date();
    return structuredClone(this.seed.settings);
  }

  listMentionGrants(): MentionGrantList {
    this.requireAuth();
    return { grants: structuredClone(this.seed.mentionGrants) };
  }

  grantMention(userId: string): Empty {
    const caller = this.requireAuth();
    if (userId === caller.id) throw validation("user_id", "you can't grant yourself mention access");
    if (!KNOWN_USER_IDS.has(userId)) throw notFound("user", `no user with id "${userId}"`);
    if (!this.seed.mentionGrants.some((g) => g.userId === userId)) {
      this.seed.mentionGrants.push({ userId, createdAt: new Date() });
    }
    return {};
  }

  revokeMention(userId: string): Empty {
    this.requireAuth();
    this.seed.mentionGrants.splice(
      0,
      this.seed.mentionGrants.length,
      ...this.seed.mentionGrants.filter((g) => g.userId !== userId),
    );
    return {};
  }

  // --------------------------------------------------------------- social --

  listFriendGroups(): FriendGroupList {
    this.requireAuth();
    return { groups: structuredClone(this.seed.friendGroups) };
  }

  createFriendGroup(req: CreateFriendGroupRequest): FriendGroup {
    this.requireAuth();
    const name = req.name.trim();
    if (!name) throw validation("name", "name must not be blank");
    const group: FriendGroup = { id: this.nextId("group"), name, members: [], createdAt: new Date() };
    this.seed.friendGroups.push(group);
    return structuredClone(group);
  }

  deleteFriendGroup(id: string): Empty {
    this.requireAuth();
    const idx = this.seed.friendGroups.findIndex((g) => g.id === id);
    if (idx === -1) throw notFound("friend_group", `no friend group with id "${id}"`);
    this.seed.friendGroups.splice(idx, 1);
    return {};
  }

  addFriend(req: AddFriendRequest): Empty {
    const caller = this.requireAuth();
    const group = this.seed.friendGroups.find((g) => g.id === req.groupId);
    if (!group) throw notFound("friend_group", `no friend group with id "${req.groupId}"`);
    if (req.userId === caller.id) throw validation("user_id", "you can't add yourself to your own friend group");
    if (!KNOWN_USER_IDS.has(req.userId)) throw notFound("user", `no user with id "${req.userId}"`);
    if (!group.members.includes(req.userId)) group.members.push(req.userId);
    return {};
  }

  removeFriend(req: RemoveFriendRequest): Empty {
    this.requireAuth();
    const group = this.seed.friendGroups.find((g) => g.id === req.groupId);
    if (!group) throw notFound("friend_group", `no friend group with id "${req.groupId}"`);
    group.members = group.members.filter((m) => m !== req.userId);
    return {};
  }
}
