// Wires `FixtureStore` (store.ts) up as an `AsyncServiceTransport` — the
// mock half of task C1's "Mock-server mode so C2-C4 develop without B"
// (PLANDOC.md §7). Structurally this mirrors firepit-api's own dispatch
// table (api/internal/server/dispatch.go): a `Record<service, Record<op,
// handler>>` keyed on the same kebab-case op names the real wire uses,
// each handler decoding a request payload with the generated codec, calling
// one `FixtureStore` method, and re-encoding the result — so every op here
// round-trips through the exact same CBOR codec C2-C4's real requests will,
// and a bug in the mock wiring looks the same as a bug in the real one.
//
// Failures surface exactly like the real transport: a `FixtureStore` method
// throws `FirepitServiceError` for an expected application failure (the
// caller sees the same typed error either way — see `~/lib/errors`); an
// unrecognized (service, op) pair throws `FirepitTransportError`, matching
// dispatch.go's "unknown service/op" transport-status outcome.
import type { AsyncServiceTransport } from "~/gen/client.async.gen";
import {
  asArray,
  asString,
  decode,
  fromBeginLoginRequestCbor,
  fromCreateBoardRequestCbor,
  fromCreateCommentRequestCbor,
  fromCreatePostRequestCbor,
  fromEditCommentRequestCbor,
  fromEditPostRequestCbor,
  fromEndorseRequestCbor,
  fromListNotificationsRequestCbor,
  fromSetMutedRequestCbor,
  fromTargetRefCbor,
  fromUpdateBoardRequestCbor,
  fromUpdateSettingsRequestCbor,
  toBeginLoginResponseCbor,
  toBoardCbor,
  toBoardPageCbor,
  toCommentCbor,
  toEmptyCbor,
  toEndorsementCbor,
  toEndorsementListCbor,
  toMentionGrantListCbor,
  toNotificationPageCbor,
  toPostCbor,
  toPostPageCbor,
  toRevisionListCbor,
  toSubscriptionCbor,
  toSubscriptionListCbor,
  toThreadCbor,
  toUnreadSummaryCbor,
  toUserProfileCbor,
  toUserSettingsCbor,
} from "~/gen/codec.gen";
import { FirepitTransportError } from "~/lib/errors";
import { methodToOp } from "~/lib/opNaming";
import { FixtureStore } from "./store";

type Handler = (payload: Uint8Array) => Uint8Array;

const bareString = (payload: Uint8Array): string => asString(decode(payload));
const bareStringArray = (payload: Uint8Array): string[] => asArray(decode(payload)).map(asString);

function buildRoutes(store: FixtureStore): Record<string, Record<string, Handler>> {
  return {
    auth: {
      "begin-login": (p) => toBeginLoginResponseCbor(store.beginLogin(fromBeginLoginRequestCbor(p).domain)),
      logout: () => {
        store.logout();
        return toEmptyCbor({});
      },
      whoami: () => toUserProfileCbor(store.whoami()),
    },
    board: {
      "list-boards": () => toBoardPageCbor(store.listBoards()),
      "get-board": (p) => toBoardCbor(store.getBoard(bareString(p))),
      "create-board": (p) => toBoardCbor(store.createBoard(fromCreateBoardRequestCbor(p))),
      "update-board": (p) => toBoardCbor(store.updateBoard(fromUpdateBoardRequestCbor(p))),
      "archive-board": (p) => toEmptyCbor(store.archiveBoard(bareString(p))),
      // No board-membership model in the mock (PLANDOC.md's board_members
      // table has no fixture data yet) — accepted as a no-op so a C2-C4 admin
      // UI can still exercise the call shape without erroring.
      "set-board-member": () => toEmptyCbor({}),
      "remove-board-member": () => toEmptyCbor({}),
    },
    thread: {
      "list-posts": (p) => toPostPageCbor(store.listPosts(fromTargetRefLikeBoardId(p))),
      "get-thread": (p) => toThreadCbor(store.getThread(bareStringField(p, "post_id"))),
      "create-post": (p) => toPostCbor(store.createPost(fromCreatePostRequestCbor(p))),
      "create-comment": (p) => toCommentCbor(store.createComment(fromCreateCommentRequestCbor(p))),
      "edit-post": (p) => {
        const req = fromEditPostRequestCbor(p);
        return toPostCbor(store.editPost(req.id, req.title, req.bodyMd));
      },
      "edit-comment": (p) => {
        const req = fromEditCommentRequestCbor(p);
        return toCommentCbor(store.editComment(req.id, req.bodyMd));
      },
      "list-revisions": (p) => toRevisionListCbor(store.listRevisions(fromTargetRefCbor(p))),
      "delete-post": (p) => toEmptyCbor(store.deletePost(bareString(p))),
      "delete-comment": (p) => toEmptyCbor(store.deleteComment(bareString(p))),
    },
    endorsement: {
      endorse: (p) => toEndorsementCbor(store.endorse(fromEndorseRequestCbor(p))),
      retract: (p) => toEmptyCbor(store.retract(fromEndorseRequestCbor(p))),
      "list-endorsements": (p) => toEndorsementListCbor(store.listEndorsements(fromTargetRefCbor(p))),
    },
    subscription: {
      subscribe: (p) => toSubscriptionCbor(store.subscribe(fromTargetRefCbor(p))),
      unsubscribe: (p) => toEmptyCbor(store.unsubscribe(fromTargetRefCbor(p))),
      "set-muted": (p) => {
        const req = fromSetMutedRequestCbor(p);
        return toSubscriptionCbor(store.setMuted({ targetType: req.targetType, targetId: req.targetId }, req.muted));
      },
      "list-subscriptions": () => toSubscriptionListCbor(store.listSubscriptions()),
    },
    read: {
      "mark-read": (p) => toEmptyCbor(store.markRead(fromTargetRefCbor(p))),
      "mark-unread": (p) => toEmptyCbor(store.markUnread(fromTargetRefCbor(p))),
      "unread-summary": () => toUnreadSummaryCbor(store.unreadSummary()),
    },
    notification: {
      "list-notifications": (p) => toNotificationPageCbor(store.listNotifications(fromListNotificationsRequestCbor(p))),
      "mark-notification-read": (p) => toEmptyCbor(store.markNotificationRead(bareStringArray(p))),
      "mark-all-read": () => toEmptyCbor(store.markAllRead()),
    },
    settings: {
      "get-settings": () => toUserSettingsCbor(store.getSettings()),
      "update-settings": (p) => toUserSettingsCbor(store.updateSettings(fromUpdateSettingsRequestCbor(p))),
      "list-mention-grants": () => toMentionGrantListCbor(store.listMentionGrants()),
      "grant-mention": (p) => toEmptyCbor(store.grantMention(bareString(p))),
      "revoke-mention": (p) => toEmptyCbor(store.revokeMention(bareString(p))),
    },
  };
}

// list-posts's request (ListPostsRequest) is a map with a `board_id` field
// (plus cursor/limit the mock doesn't paginate on) — there's no standalone
// generated decoder for just that field, so pull it out with the low-level
// map helpers the same way the generated codec's own bare-type request
// handling does.
function fromTargetRefLikeBoardId(payload: Uint8Array): string {
  return bareStringField(payload, "board_id");
}

function bareStringField(payload: Uint8Array, key: string): string {
  const value = decode(payload);
  if (!(value instanceof Map)) throw new Error(`expected a map decoding field "${key}"`);
  const field = value.get(key);
  if (field === undefined) throw new Error(`missing required field: ${key}`);
  return asString(field);
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
  return {
    store,
    // No `await` in the body: `AsyncServiceTransport`'s contract is async,
    // but the mock has no real I/O to await — the `async` keyword just
    // makes the return type `Promise<Uint8Array>` as required.
    async call(service: string, op: string, payload: Uint8Array): Promise<Uint8Array> {
      const kebabOp = methodToOp(op);
      const handler = routes[service]?.[kebabOp];
      if (!handler) {
        throw new FirepitTransportError(`unknown (service, op): ${service}/${kebabOp}`);
      }
      return handler(payload);
    },
  };
}
