// Run fixture operations through the same codecs and reply variants as the API.
import type { AsyncServiceTransport } from "~/gen/client.async.gen";
import {
  type RpcReply,
  encodeAuthServiceBeginLoginReply,
  encodeAuthServiceWhoamiReply,
  encodeBoardServiceArchiveBoardReply,
  encodeBoardServiceCreateBoardReply,
  encodeBoardServiceGetBoardReply,
  encodeBoardServiceRemoveBoardMemberReply,
  encodeBoardServiceSetBoardMemberReply,
  encodeBoardServiceUpdateBoardReply,
  encodeCategoryServiceCreateCategoryReply,
  encodeCategoryServiceDeleteCategoryReply,
  encodeCategoryServiceUpdateCategoryReply,
  encodeEndorsementServiceEndorseReply,
  encodeEndorsementServiceRetractReply,
  encodeSettingsServiceGrantMentionReply,
  encodeSettingsServiceRevokeMentionReply,
  encodeSettingsServiceUpdateSettingsReply,
  encodeSocialServiceAddFriendReply,
  encodeSocialServiceCreateFriendGroupReply,
  encodeSocialServiceDeleteFriendGroupReply,
  encodeSocialServiceRemoveFriendReply,
  encodeSocialServiceResolveUserReply,
  encodeSubscriptionServiceSetMutedReply,
  encodeSubscriptionServiceSubscribeReply,
  encodeSubscriptionServiceUnsubscribeReply,
  encodeThreadServiceCreateCommentReply,
  encodeThreadServiceCreatePostReply,
  encodeThreadServiceDeleteCommentReply,
  encodeThreadServiceDeletePostReply,
  encodeThreadServiceEditCommentReply,
  encodeThreadServiceEditPostReply,
  asArray,
  asString,
  decode,
  fromAddFriendRequestCbor,
  fromBeginLoginRequestCbor,
  fromCreateBoardRequestCbor,
  fromCreateCategoryRequestCbor,
  fromCreateCommentRequestCbor,
  fromCreateFriendGroupRequestCbor,
  fromCreatePostRequestCbor,
  fromEditCommentRequestCbor,
  fromEditPostRequestCbor,
  fromDeleteCategoryRequestCbor,
  fromEndorseRequestCbor,
  fromGetThreadRequestCbor,
  fromListNotificationsRequestCbor,
  fromListPostsRequestCbor,
  fromRemoveFriendRequestCbor,
  fromSetMutedRequestCbor,
  fromTargetRefCbor,
  fromUpdateBoardRequestCbor,
  fromUpdateCategoryRequestCbor,
  fromUpdateSettingsRequestCbor,
  toBoardPageCbor,
  toCategoryListCbor,
  toEmptyCbor,
  toEndorsementListCbor,
  toFriendGroupListCbor,
  toMentionGrantListCbor,
  toNotificationPageCbor,
  toPostPageCbor,
  toRevisionListCbor,
  toSubscriptionListCbor,
  toThreadCbor,
  toUnreadSummaryCbor,
  toUserSettingsCbor,
} from "~/gen/codec.gen";
import { FirepitTransportError } from "~/lib/errors";
import { methodToOp, serviceToWire } from "~/lib/opNaming";
import { FixtureStore } from "./store";

type Handler = (payload: Uint8Array) => RpcReply | Promise<RpcReply>;

const bareString = (payload: Uint8Array): string => asString(decode(payload));
const bareStringArray = (payload: Uint8Array): string[] => asArray(decode(payload)).map(asString);

function buildRoutes(store: FixtureStore): Record<string, Record<string, Handler>> {
  return {
    auth: {
      "begin-login": (p) => encodeAuthServiceBeginLoginReply(() => store.beginLogin(fromBeginLoginRequestCbor(p).domain)),
      logout: () => {
        store.logout();
        return { variant: "Empty", payload: toEmptyCbor({}) };
      },
      whoami: () => encodeAuthServiceWhoamiReply(() => store.whoami()),
    },
    board: {
      "list-boards": () => ({ variant: "BoardPage", payload: toBoardPageCbor(store.listBoards()) }),
      "get-board": (p) => encodeBoardServiceGetBoardReply(() => store.getBoard(bareString(p))),
      "create-board": (p) => encodeBoardServiceCreateBoardReply(() => store.createBoard(fromCreateBoardRequestCbor(p))),
      "update-board": (p) => encodeBoardServiceUpdateBoardReply(() => store.updateBoard(fromUpdateBoardRequestCbor(p))),
      "archive-board": (p) => encodeBoardServiceArchiveBoardReply(() => store.archiveBoard(bareString(p))),
      // No board-membership model in the mock (PLANDOC.md's board_members
      // table has no fixture data yet) — accepted as a no-op so a C2-C4 admin
      // UI can still exercise the call shape without erroring.
      "set-board-member": () => encodeBoardServiceSetBoardMemberReply(() => ({})),
      "remove-board-member": () => encodeBoardServiceRemoveBoardMemberReply(() => ({})),
    },
    category: {
      "list-board-categories": (p) => ({ variant: "CategoryList", payload: toCategoryListCbor(store.listBoardCategories(bareString(p))) }),
      "create-category": (p) => encodeCategoryServiceCreateCategoryReply(() => store.createCategory(fromCreateCategoryRequestCbor(p))),
      "update-category": (p) => encodeCategoryServiceUpdateCategoryReply(() => store.updateCategory(fromUpdateCategoryRequestCbor(p))),
      "delete-category": (p) => encodeCategoryServiceDeleteCategoryReply(() => store.deleteCategory(fromDeleteCategoryRequestCbor(p))),
      "list-categories": () => ({ variant: "CategoryList", payload: toCategoryListCbor(store.listCategories()) }),
    },
    thread: {
      "list-posts": (p) => ({ variant: "PostPage", payload: toPostPageCbor(store.listPosts(fromListPostsRequestCbor(p))) }),
      "get-thread": (p) => ({ variant: "Thread", payload: toThreadCbor(store.getThread(fromGetThreadRequestCbor(p).postId)) }),
      "create-post": (p) => encodeThreadServiceCreatePostReply(() => store.createPost(fromCreatePostRequestCbor(p))),
      "create-comment": (p) => encodeThreadServiceCreateCommentReply(() => store.createComment(fromCreateCommentRequestCbor(p))),
      "edit-post": (p) => {
        const req = fromEditPostRequestCbor(p);
        return encodeThreadServiceEditPostReply(() => store.editPost(req.id, req.title, req.bodyMd, req.categoryIds));
      },
      "edit-comment": (p) => {
        const req = fromEditCommentRequestCbor(p);
        return encodeThreadServiceEditCommentReply(() => store.editComment(req.id, req.bodyMd));
      },
      "list-revisions": (p) => ({ variant: "RevisionList", payload: toRevisionListCbor(store.listRevisions(fromTargetRefCbor(p))) }),
      "delete-post": (p) => encodeThreadServiceDeletePostReply(() => store.deletePost(bareString(p))),
      "delete-comment": (p) => encodeThreadServiceDeleteCommentReply(() => store.deleteComment(bareString(p))),
    },
    endorsement: {
      endorse: (p) => encodeEndorsementServiceEndorseReply(() => store.endorse(fromEndorseRequestCbor(p))),
      retract: (p) => encodeEndorsementServiceRetractReply(() => store.retract(fromEndorseRequestCbor(p))),
      "list-endorsements": (p) => ({ variant: "EndorsementList", payload: toEndorsementListCbor(store.listEndorsements(fromTargetRefCbor(p))) }),
    },
    subscription: {
      subscribe: (p) => encodeSubscriptionServiceSubscribeReply(() => store.subscribe(fromTargetRefCbor(p))),
      unsubscribe: (p) => encodeSubscriptionServiceUnsubscribeReply(() => store.unsubscribe(fromTargetRefCbor(p))),
      "set-muted": (p) => {
        const req = fromSetMutedRequestCbor(p);
        return encodeSubscriptionServiceSetMutedReply(() => store.setMuted({ targetType: req.targetType, targetId: req.targetId }, req.muted));
      },
      "list-subscriptions": () => ({ variant: "SubscriptionList", payload: toSubscriptionListCbor(store.listSubscriptions()) }),
    },
    read: {
      "mark-read": (p) => ({ variant: "Empty", payload: toEmptyCbor(store.markRead(fromTargetRefCbor(p))) }),
      "mark-unread": (p) => ({ variant: "Empty", payload: toEmptyCbor(store.markUnread(fromTargetRefCbor(p))) }),
      "unread-summary": () => ({ variant: "UnreadSummary", payload: toUnreadSummaryCbor(store.unreadSummary()) }),
    },
    notification: {
      "list-notifications": (p) => ({ variant: "NotificationPage", payload: toNotificationPageCbor(store.listNotifications(fromListNotificationsRequestCbor(p))) }),
      "mark-notification-read": (p) => ({ variant: "Empty", payload: toEmptyCbor(store.markNotificationRead(bareStringArray(p))) }),
      "mark-all-read": () => ({ variant: "Empty", payload: toEmptyCbor(store.markAllRead()) }),
    },
    settings: {
      "get-settings": () => ({ variant: "UserSettings", payload: toUserSettingsCbor(store.getSettings()) }),
      "update-settings": (p) => encodeSettingsServiceUpdateSettingsReply(() => store.updateSettings(fromUpdateSettingsRequestCbor(p))),
      "list-mention-grants": () => ({ variant: "MentionGrantList", payload: toMentionGrantListCbor(store.listMentionGrants()) }),
      "grant-mention": (p) => encodeSettingsServiceGrantMentionReply(() => store.grantMention(bareString(p))),
      "revoke-mention": (p) => encodeSettingsServiceRevokeMentionReply(() => store.revokeMention(bareString(p))),
    },
    social: {
      "list-friend-groups": () => ({ variant: "FriendGroupList", payload: toFriendGroupListCbor(store.listFriendGroups()) }),
      "create-friend-group": (p) => encodeSocialServiceCreateFriendGroupReply(() => store.createFriendGroup(fromCreateFriendGroupRequestCbor(p))),
      "delete-friend-group": (p) => encodeSocialServiceDeleteFriendGroupReply(() => store.deleteFriendGroup(bareString(p))),
      "add-friend": (p) => encodeSocialServiceAddFriendReply(() => store.addFriend(fromAddFriendRequestCbor(p))),
      "remove-friend": (p) => encodeSocialServiceRemoveFriendReply(() => store.removeFriend(fromRemoveFriendRequestCbor(p))),
      // Schema follow-up (grant-mention/add-friend by handle instead of a
      // raw pasted ULID): resolve-user turns a typed handle into the
      // matching UserProfile.
      "resolve-user": (p) => encodeSocialServiceResolveUserReply(() => store.resolveUser(bareString(p))),
    },
  };
}

export interface MockTransport extends AsyncServiceTransport {
  readonly store: FixtureStore;
}

/**
 * Build a fresh mock transport. Pass a `FixtureStore` to share/inspect state
 * (tests do this to assert on mutations, and get a clean, storage-free
 * store every time); the zero-arg default — what `src/lib/api.ts`'s
 * singleton uses in the real app — turns on `persistLogin` so a real
 * `window.location.href` login round-trip (session.tsx's `login()`) survives
 * the full-page reload it causes (see `FixtureStoreOptions.persistLogin`'s
 * doc comment).
 */
export function createMockTransport(store: FixtureStore = new FixtureStore(undefined, { persistLogin: true })): MockTransport {
  const routes = buildRoutes(store);
  async function callReply(service: string, op: string, payload: Uint8Array): Promise<RpcReply> {
    const handler = routes[serviceToWire(service)]?.[methodToOp(op)];
    if (!handler) {
      throw new FirepitTransportError(`unknown (service, op): ${service}/${op}`);
    }
    return handler(payload);
  }

  return {
    store,
    callReply,
    async call(service, op, payload) {
      return (await callReply(service, op, payload)).payload;
    },
  };
}
