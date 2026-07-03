// Deterministic seed data for the mock CSIL transport (task C1, PLANDOC.md
// §7 "Mock-server mode so C2-C4 develop without B"). `createSeed()` returns
// a brand-new, independent snapshot every call — component tests import it
// directly to get isolated state per test; `src/lib/mock/store.ts` wraps one
// snapshot in the mutation logic the mock transport dispatches onto.
//
// Every id below is a fixed, human-readable fake ULID (real ULIDs are
// base32, 26 chars — these are shaped the same but spell out what they name
// so fixtures read as documentation). Nothing here needs to be a *valid*
// ULID: the generated types alias every id to `string`, and nothing
// downstream parses it.
import type {
  Board,
  Comment,
  Endorsement,
  FriendGroup,
  MentionGrant,
  Notification,
  Post,
  Revision,
  Subscription,
  UserProfile,
  UserSettings,
} from "~/gen/types.gen";

const DAY_MS = 24 * 60 * 60 * 1000;
const NOW = new Date("2026-07-03T15:00:00Z");
const ago = (days: number, hours = 0): Date => new Date(NOW.getTime() - days * DAY_MS - hours * 60 * 60 * 1000);

// --- users -----------------------------------------------------------------
//
// There is no UserService in csil/firepit.csil — authors/endorsers are
// referenced by id only (Post.authorId, Endorsement.userId, ...), and CSIL
// has no op to resolve one to a display name yet. The mock only needs full
// profiles for the *caller* (whoami's return type); everyone else is just an
// id + the handle baked into fixture content itself (e.g. "@carol" in body
// markdown), same limitation the real API has today.

export const MOCK_USER_ID = "01FPMOCKUSERALICE0000000";
// Exported (task C4, PLANDOC.md §7): the SettingsService/SocialService mock
// (src/lib/mock/store.ts) needs *some* notion of "a user id that actually
// exists" to validate grant-mention/add-friend the way the real backend's
// GetUser check does — these three are the only other users the fixture
// store knows about. Still no handle for any of them (see this module's own
// doc comment above) — that limitation is unchanged, just now referenced
// from two files instead of one.
export const BOB_ID = "01FPMOCKUSERBOB00000000";
export const CAROL_ID = "01FPMOCKUSERCAROL0000000";
export const DAVE_ID = "01FPMOCKUSERDAVE00000000";
export const OTHER_USER_IDS = [BOB_ID, CAROL_ID, DAVE_ID] as const;

export const MOCK_USER: UserProfile = {
  id: MOCK_USER_ID,
  linkkeysDomain: "todandlorna.com",
  handle: "alice",
  displayName: "Alice Anders",
  kind: "human",
  roles: [],
  createdAt: ago(400),
};

// --- boards ------------------------------------------------------------------

const BOARD_FIREPIT = "01FPMOCKBOARDFIREPIT0000";
const BOARD_ANNOUNCE = "01FPMOCKBOARDANNOUNCE000";
const BOARD_CSILGEN = "01FPMOCKBOARDCSILGEN0000";

export const boards: readonly Board[] = [
  {
    id: BOARD_FIREPIT,
    slug: "firepit",
    title: "Firepit Meta",
    description: "General discussion about firepit itself — the forum eating its own dog food.",
    kind: "discussion",
    createdBy: CAROL_ID,
    createdAt: ago(120),
  },
  {
    id: BOARD_ANNOUNCE,
    slug: "announcements",
    title: "Announcements",
    description: "Release notes and project news. Anyone can reply; only maintainers post roots.",
    kind: "announce",
    createdBy: CAROL_ID,
    createdAt: ago(120),
  },
  {
    id: BOARD_CSILGEN,
    slug: "csilgen",
    title: "csilgen",
    description: "Schema/codegen discussion for the CSIL IDL and its generated clients.",
    kind: "discussion",
    createdBy: DAVE_ID,
    createdAt: ago(90),
  },
];

// --- posts + comments --------------------------------------------------------
//
// One deep thread (6 levels) on the welcome post, so a tree/collapse UI has
// something real to render; a couple of shallower posts elsewhere so list
// views aren't a single-item degenerate case.

const POST_WELCOME = "01FPMOCKPOSTWELCOME00000";
const POST_RELEASE = "01FPMOCKPOSTRELEASE00000";
const POST_CSIL_QUESTION = "01FPMOCKPOSTCSILQ0000000";
const POST_GH_ISSUE = "01FPMOCKPOSTGHISSUE00000";
const POST_SECOND = "01FPMOCKPOSTSECOND000000";

export const posts: readonly Post[] = [
  {
    id: POST_WELCOME,
    boardId: BOARD_FIREPIT,
    authorId: CAROL_ID,
    title: "Welcome to Firepit",
    bodyMd:
      "This is the first post. Threaded replies go arbitrarily deep — try " +
      "collapsing the tree below once C3 lands the thread view.",
    origin: "user",
    commentCount: 6,
    lastActivityAt: ago(0, 2),
    createdAt: ago(10),
  },
  {
    id: POST_RELEASE,
    boardId: BOARD_ANNOUNCE,
    authorId: CAROL_ID,
    title: "v0.1.0 released",
    bodyMd: "Skeleton milestone (M1) is deployed. Login works, stub API answers. Full changelog in the repo.",
    origin: "system",
    originRef: JSON.stringify({ tag: "v0.1.0" }),
    commentCount: 1,
    lastActivityAt: ago(1),
    createdAt: ago(2),
  },
  {
    id: POST_CSIL_QUESTION,
    boardId: BOARD_CSILGEN,
    authorId: DAVE_ID,
    title: "Why kebab-case ops on the wire?",
    bodyMd: "Curious about the reasoning — is this documented anywhere beyond the transport conventions doc?",
    origin: "user",
    commentCount: 0,
    lastActivityAt: ago(5),
    createdAt: ago(5),
  },
  // Two more on BOARD_FIREPIT (additive — task C2, PLANDOC.md §7): the
  // welcome post alone left every board with exactly one post, which is too
  // degenerate a fixture to exercise list-posts' cursor pagination (C2's
  // "post list pagination appends" accept criterion) or render a
  // GitHub-origin row (C2's "small origin glyph" requirement). Both stay
  // deterministic, same as every other fixture here.
  {
    id: POST_GH_ISSUE,
    boardId: BOARD_FIREPIT,
    authorId: DAVE_ID,
    title: "flaky ltree GIST index test on CI",
    bodyMd: "Ingested from the firepit repo's issue tracker — see the linked issue for repro steps.",
    origin: "github",
    originRef: JSON.stringify({ repo: "catalystcommunity/firepit", issue: 42 }),
    commentCount: 0,
    lastActivityAt: ago(1),
    createdAt: ago(4),
  },
  {
    id: POST_SECOND,
    boardId: BOARD_FIREPIT,
    authorId: BOB_ID,
    title: "Style guide for board descriptions?",
    bodyMd: "Should descriptions be a sentence or a short paragraph? Looking at csilgen's board for reference.",
    origin: "user",
    commentCount: 0,
    lastActivityAt: ago(3),
    createdAt: ago(7),
  },
];

const COMMENT_1 = "01FPMOCKCOMMENT000000001";
const COMMENT_2 = "01FPMOCKCOMMENT000000002";
const COMMENT_3 = "01FPMOCKCOMMENT000000003";
const COMMENT_4 = "01FPMOCKCOMMENT000000004";
const COMMENT_5 = "01FPMOCKCOMMENT000000005";
const COMMENT_6 = "01FPMOCKCOMMENT000000006";
const COMMENT_RELEASE_1 = "01FPMOCKCOMMENTRELEASE01";

export const comments: readonly Comment[] = [
  {
    id: COMMENT_1,
    postId: POST_WELCOME,
    authorId: BOB_ID,
    bodyMd: "Glad to be here. First!",
    origin: "user",
    createdAt: ago(9, 20),
  },
  {
    id: COMMENT_2,
    postId: POST_WELCOME,
    parentCommentId: COMMENT_1,
    authorId: CAROL_ID,
    bodyMd: "Welcome @bob — feel free to open the first real discussion thread.",
    origin: "user",
    createdAt: ago(9, 10),
  },
  {
    id: COMMENT_3,
    postId: POST_WELCOME,
    parentCommentId: COMMENT_2,
    authorId: DAVE_ID,
    bodyMd: "Does mailing-list flat view show the same ordering as the tree?",
    origin: "user",
    createdAt: ago(8),
  },
  {
    id: COMMENT_4,
    postId: POST_WELCOME,
    parentCommentId: COMMENT_3,
    authorId: MOCK_USER_ID,
    bodyMd: "Yes — same depth-first order, just rendered without indentation.",
    origin: "user",
    createdAt: ago(6),
  },
  {
    id: COMMENT_5,
    postId: POST_WELCOME,
    parentCommentId: COMMENT_4,
    authorId: BOB_ID,
    bodyMd: "Nice, that matches how the old mailing lists read.",
    origin: "user",
    createdAt: ago(3),
  },
  {
    id: COMMENT_6,
    postId: POST_WELCOME,
    parentCommentId: COMMENT_5,
    authorId: CAROL_ID,
    bodyMd: "Six levels deep and still legible — that's the bar.",
    origin: "user",
    editedAt: ago(0, 4),
    createdAt: ago(0, 2),
  },
  {
    id: COMMENT_RELEASE_1,
    postId: POST_RELEASE,
    authorId: BOB_ID,
    bodyMd: "Congrats on shipping!",
    origin: "user",
    createdAt: ago(1),
  },
];

// --- endorsements -------------------------------------------------------------

export const endorsements: readonly Endorsement[] = [
  {
    id: "01FPMOCKENDORSE00000001",
    userId: CAROL_ID,
    targetType: "post",
    targetId: POST_WELCOME,
    roleBadge: "maintainer",
    createdAt: ago(9),
  },
  {
    id: "01FPMOCKENDORSE00000002",
    userId: MOCK_USER_ID,
    targetType: "comment",
    targetId: COMMENT_2,
    createdAt: ago(8),
  },
  {
    id: "01FPMOCKENDORSE00000003",
    userId: DAVE_ID,
    targetType: "comment",
    targetId: COMMENT_4,
    createdAt: ago(5),
  },
];

// --- subscriptions (the mock caller's own) -------------------------------------

export const subscriptions: readonly Subscription[] = [
  {
    id: "01FPMOCKSUBSCRIBE0000001",
    targetType: "board",
    targetId: BOARD_FIREPIT,
    muted: false,
    createdAt: ago(30),
  },
  {
    id: "01FPMOCKSUBSCRIBE0000002",
    targetType: "post",
    targetId: POST_RELEASE,
    muted: false,
    createdAt: ago(2),
  },
  {
    // A comment-subtree subscription, muted — task C4's subscriptions
    // management (SettingsPage) needs at least one of every target_type to
    // render its grouped sections, and a muted one to exercise the
    // mute/unmute toggle from a non-default starting state.
    id: "01FPMOCKSUBSCRIBE0000003",
    targetType: "comment",
    targetId: COMMENT_3,
    muted: true,
    createdAt: ago(7),
  },
];

// --- notifications (the mock caller's own inbox) -------------------------------
//
// One of every NotificationEvent (new_post/new_comment/mention/github_event
// — csil/types/notifications.csil's enum; there is no separate "endorsed"
// event today, endorsement notifications aren't modeled as their own kind
// yet), a mix of read/unread, and enough rows (7) that NotificationsPage's
// PAGE_SIZE (5) has something real to paginate through.

export const notifications: readonly Notification[] = [
  {
    id: "01FPMOCKNOTIFY0000000001",
    event: "new_comment",
    actorId: CAROL_ID,
    targetType: "comment",
    targetId: COMMENT_6,
    postId: POST_WELCOME,
    createdAt: ago(0, 2),
  },
  {
    id: "01FPMOCKNOTIFY0000000002",
    event: "mention",
    actorId: DAVE_ID,
    targetType: "comment",
    targetId: COMMENT_3,
    postId: POST_WELCOME,
    createdAt: ago(8),
  },
  {
    id: "01FPMOCKNOTIFY0000000003",
    event: "github_event",
    targetType: "post",
    targetId: POST_RELEASE,
    postId: POST_RELEASE,
    readAt: ago(1),
    createdAt: ago(2),
  },
  {
    id: "01FPMOCKNOTIFY0000000004",
    event: "new_post",
    actorId: DAVE_ID,
    targetType: "post",
    targetId: POST_CSIL_QUESTION,
    postId: POST_CSIL_QUESTION,
    createdAt: ago(5),
  },
  {
    id: "01FPMOCKNOTIFY0000000005",
    event: "new_comment",
    actorId: BOB_ID,
    targetType: "comment",
    targetId: COMMENT_RELEASE_1,
    postId: POST_RELEASE,
    readAt: ago(0, 20),
    createdAt: ago(1),
  },
  {
    id: "01FPMOCKNOTIFY0000000006",
    event: "mention",
    actorId: CAROL_ID,
    targetType: "comment",
    targetId: COMMENT_2,
    postId: POST_WELCOME,
    readAt: ago(6),
    createdAt: ago(9),
  },
  {
    // No actorId: a system-authored event (the release post itself has no
    // single human actor) — exercises the "the project" fallback in
    // src/lib/notifications.ts's actorLabel() rather than only ever
    // rendering a real user id.
    id: "01FPMOCKNOTIFY0000000007",
    event: "new_post",
    targetType: "post",
    targetId: POST_RELEASE,
    postId: POST_RELEASE,
    readAt: ago(1, 12),
    createdAt: ago(2, 1),
  },
];

// --- settings + mention grants (the mock caller's own) --------------------------

export const settings: UserSettings = {
  mentionPolicy: "subscribed",
  notifyOnEndorse: true,
  updatedAt: ago(30),
};

export const mentionGrants: readonly MentionGrant[] = [{ userId: BOB_ID, createdAt: ago(20) }];

// --- friend groups (the mock caller's own, private) -----------------------------

export const FRIEND_GROUP_CORE = "01FPMOCKGROUPCORE0000000";

export const friendGroups: readonly FriendGroup[] = [
  {
    id: FRIEND_GROUP_CORE,
    name: "Core reviewers",
    members: [CAROL_ID, DAVE_ID],
    createdAt: ago(60),
  },
];

/** A fresh, independent deep copy of every seed table — see the module doc. */
export function createSeed() {
  return {
    user: structuredClone(MOCK_USER),
    boards: structuredClone(boards) as Board[],
    posts: structuredClone(posts) as Post[],
    comments: structuredClone(comments) as Comment[],
    endorsements: structuredClone(endorsements) as Endorsement[],
    subscriptions: structuredClone(subscriptions) as Subscription[],
    notifications: structuredClone(notifications) as Notification[],
    settings: structuredClone(settings) as UserSettings,
    mentionGrants: structuredClone(mentionGrants) as MentionGrant[],
    friendGroups: structuredClone(friendGroups) as FriendGroup[],
    revisions: [] as Revision[],
  };
}

export type Seed = ReturnType<typeof createSeed>;
