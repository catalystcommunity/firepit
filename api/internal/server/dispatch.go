// Package server wires firepit-api's HTTP surface: the CSIL-RPC dispatcher
// at POST /csil/v1/rpc, session middleware, /healthz, CORS, and request
// logging (task B1, PLANDOC.md §3, §7). This file owns the (service, op)
// routing table; server.go owns the http.Server/middleware chain; session.go
// owns the session-cookie-to-user middleware.
//
// # Wire naming for (service, op)
//
// There is no generated Route<Service>Channel/dispatch function in
// api/internal/csil — csilgen's bare `go` target (see tools.sh's `cmd_gen`)
// only emits types, per-op codec functions, and service interfaces, not a
// router. This file is that missing piece, hand-written once here rather
// than regenerated per service.
//
// The (service, op) strings a real request carries are NOT the PascalCase
// interface/method names or the CSIL schema's `AuthService`-with-suffix
// service identifiers — they're what csilgen's *-client generators actually
// derive and what the one real, test-verified precedent in this org
// (longhouse's TypeScript transport, webapp/src/transport/csilrpc.ts there)
// puts on the wire:
//
//   - service: the CSIL service name with a trailing "Service" stripped and
//     lowercased ("AuthService" -> "auth"; see clients/go/client.gen.go,
//     which already calls transport.Call(ctx, "auth", "BeginLogin", ...)).
//   - op: the operation name exactly as declared in csil/firepit.csil,
//     kebab-case ("begin-login") — csilgen's generated clients hand a
//     PascalCase method name to the Transport seam and expect the carrier to
//     kebab-case it before it hits the wire (longhouse's methodToOp); the
//     routing table below is keyed directly on the already-kebab-case op
//     names from the schema, so no runtime conversion is needed server-side.
//
// This is the exact pairing exercised end-to-end by longhouse's own tests
// (webapp/src/transport/csilrpc.test.ts there), so it's the precedent this
// server follows rather than inventing a third convention.
package server

import (
	"context"
	"errors"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/csilservices"
	"github.com/catalystcommunity/firepit/api/internal/transport"
)

// Services bundles one implementation per generated csil service interface.
// main.go constructs this once at boot (today, every field is a
// csilservices.NewXService stub; B2-B9 replace individual fields with real
// implementations — the type doesn't change).
type Services struct {
	Auth         csil.AuthService
	Board        csil.BoardService
	Category     csil.CategoryService
	Thread       csil.ThreadService
	Endorsement  csil.EndorsementService
	Settings     csil.SettingsService
	Social       csil.SocialService
	Subscription csil.SubscriptionService
	Read         csil.ReadService
	Notification csil.NotificationService
	Integration  csil.IntegrationService
}

// typedHandler decodes a request payload, calls a service method, and
// encodes the result (or maps a failure) to a transport.HandlerOutcome. It
// is the per-op unit the routing table below is built from.
type typedHandler func(ctx context.Context, payload []byte) transport.HandlerOutcome

// routeFallible wires an operation whose CSIL declaration carries a `/
// ServiceError` arm. A *csilservices.AppError returned by fn is encoded as
// the typed ServiceError success-arm; any other error (or an *AppError on an
// op that has no such arm — see routeInfallible) is never assumed safe to
// show a caller and becomes a transport-level internal failure instead. See
// api/internal/csilservices's package doc comment for the full contract.
func routeFallible[Req any, Resp any](
	decode func([]byte) (Req, error),
	fn func(context.Context, Req) (Resp, error),
	encode func(Resp) []byte,
	variant string,
) typedHandler {
	return func(ctx context.Context, payload []byte) transport.HandlerOutcome {
		req, err := decode(payload)
		if err != nil {
			return transport.Transport(transport.StatusMalformedEnvelope, "decode "+variant+" request: "+err.Error())
		}
		resp, err := fn(ctx, req)
		if err != nil {
			var appErr *csilservices.AppError
			if errors.As(err, &appErr) {
				return transport.Reply("ServiceError", csil.EncodeServiceError(appErr.ServiceError()))
			}
			log.WithError(err).WithField("variant", variant).Error("unhandled error from service method")
			return transport.Transport(transport.StatusInternal, "internal error")
		}
		return transport.Reply(variant, encode(resp))
	}
}

// routeInfallible wires an operation whose CSIL declaration has NO `/
// ServiceError` arm: there is no typed channel to carry a failure, so any
// non-nil error from fn (whatever its type) becomes a transport-level
// internal failure. See api/internal/csilservices's package doc comment.
func routeInfallible[Req any, Resp any](
	decode func([]byte) (Req, error),
	fn func(context.Context, Req) (Resp, error),
	encode func(Resp) []byte,
	variant string,
) typedHandler {
	return func(ctx context.Context, payload []byte) transport.HandlerOutcome {
		req, err := decode(payload)
		if err != nil {
			return transport.Transport(transport.StatusMalformedEnvelope, "decode "+variant+" request: "+err.Error())
		}
		resp, err := fn(ctx, req)
		if err != nil {
			log.WithError(err).WithField("variant", variant).Error("unhandled error from service method (no declared error arm)")
			return transport.Transport(transport.StatusInternal, "internal error")
		}
		return transport.Reply(variant, encode(resp))
	}
}

// buildRoutes constructs the full (service, op) routing table from svcs. One
// row per operation declared in csil/firepit.csil, in service-declaration
// order.
func buildRoutes(svcs Services) map[string]map[string]typedHandler {
	return map[string]map[string]typedHandler{
		"auth": {
			"begin-login": routeFallible(csil.DecodeBeginLoginRequest, svcs.Auth.BeginLogin, csil.EncodeBeginLoginResponse, "BeginLoginResponse"),
			"logout":      routeInfallible(csil.DecodeEmpty, svcs.Auth.Logout, csil.EncodeEmpty, "Empty"),
			"whoami":      routeFallible(csil.DecodeEmpty, svcs.Auth.Whoami, csil.EncodeUserProfile, "UserProfile"),
		},
		"board": {
			"list-boards":         routeInfallible(csil.DecodeListBoardsRequest, svcs.Board.ListBoards, csil.EncodeBoardPage, "BoardPage"),
			"get-board":           routeFallible(csil.DecodeBoardGetBoardRequest, svcs.Board.GetBoard, csil.EncodeBoard, "Board"),
			"create-board":        routeFallible(csil.DecodeCreateBoardRequest, svcs.Board.CreateBoard, csil.EncodeBoard, "Board"),
			"update-board":        routeFallible(csil.DecodeUpdateBoardRequest, svcs.Board.UpdateBoard, csil.EncodeBoard, "Board"),
			"archive-board":       routeFallible(csil.DecodeBoardArchiveBoardRequest, svcs.Board.ArchiveBoard, csil.EncodeEmpty, "Empty"),
			"set-board-member":    routeFallible(csil.DecodeSetBoardMemberRequest, svcs.Board.SetBoardMember, csil.EncodeEmpty, "Empty"),
			"remove-board-member": routeFallible(csil.DecodeRemoveBoardMemberRequest, svcs.Board.RemoveBoardMember, csil.EncodeEmpty, "Empty"),
		},
		"category": {
			"list-board-categories": routeInfallible(csil.DecodeCategoryListBoardCategoriesRequest, svcs.Category.ListBoardCategories, csil.EncodeCategoryList, "CategoryList"),
			"create-category":       routeFallible(csil.DecodeCreateCategoryRequest, svcs.Category.CreateCategory, csil.EncodeCategory, "Category"),
			"update-category":       routeFallible(csil.DecodeUpdateCategoryRequest, svcs.Category.UpdateCategory, csil.EncodeCategory, "Category"),
			"delete-category":       routeFallible(csil.DecodeDeleteCategoryRequest, svcs.Category.DeleteCategory, csil.EncodeEmpty, "Empty"),
			"list-categories":       routeInfallible(csil.DecodeEmpty, svcs.Category.ListCategories, csil.EncodeCategoryList, "CategoryList"),
		},
		"thread": {
			"list-posts":     routeInfallible(csil.DecodeListPostsRequest, svcs.Thread.ListPosts, csil.EncodePostPage, "PostPage"),
			"get-thread":     routeInfallible(csil.DecodeGetThreadRequest, svcs.Thread.GetThread, csil.EncodeThread, "Thread"),
			"create-post":    routeFallible(csil.DecodeCreatePostRequest, svcs.Thread.CreatePost, csil.EncodePost, "Post"),
			"create-comment": routeFallible(csil.DecodeCreateCommentRequest, svcs.Thread.CreateComment, csil.EncodeComment, "Comment"),
			"edit-post":      routeFallible(csil.DecodeEditPostRequest, svcs.Thread.EditPost, csil.EncodePost, "Post"),
			"edit-comment":   routeFallible(csil.DecodeEditCommentRequest, svcs.Thread.EditComment, csil.EncodeComment, "Comment"),
			"list-revisions": routeInfallible(csil.DecodeTargetRef, svcs.Thread.ListRevisions, csil.EncodeRevisionList, "RevisionList"),
			"delete-post":    routeFallible(csil.DecodeThreadDeletePostRequest, svcs.Thread.DeletePost, csil.EncodeEmpty, "Empty"),
			"delete-comment": routeFallible(csil.DecodeThreadDeleteCommentRequest, svcs.Thread.DeleteComment, csil.EncodeEmpty, "Empty"),
		},
		"endorsement": {
			"endorse":           routeFallible(csil.DecodeEndorseRequest, svcs.Endorsement.Endorse, csil.EncodeEndorsement, "Endorsement"),
			"retract":           routeFallible(csil.DecodeEndorseRequest, svcs.Endorsement.Retract, csil.EncodeEmpty, "Empty"),
			"list-endorsements": routeInfallible(csil.DecodeTargetRef, svcs.Endorsement.ListEndorsements, csil.EncodeEndorsementList, "EndorsementList"),
		},
		"settings": {
			"get-settings":        routeInfallible(csil.DecodeEmpty, svcs.Settings.GetSettings, csil.EncodeUserSettings, "UserSettings"),
			"update-settings":     routeFallible(csil.DecodeUpdateSettingsRequest, svcs.Settings.UpdateSettings, csil.EncodeUserSettings, "UserSettings"),
			"list-mention-grants": routeInfallible(csil.DecodeEmpty, svcs.Settings.ListMentionGrants, csil.EncodeMentionGrantList, "MentionGrantList"),
			"grant-mention":       routeFallible(csil.DecodeSettingsGrantMentionRequest, svcs.Settings.GrantMention, csil.EncodeEmpty, "Empty"),
			"revoke-mention":      routeFallible(csil.DecodeSettingsRevokeMentionRequest, svcs.Settings.RevokeMention, csil.EncodeEmpty, "Empty"),
		},
		"social": {
			"list-friend-groups":  routeInfallible(csil.DecodeEmpty, svcs.Social.ListFriendGroups, csil.EncodeFriendGroupList, "FriendGroupList"),
			"create-friend-group": routeFallible(csil.DecodeCreateFriendGroupRequest, svcs.Social.CreateFriendGroup, csil.EncodeFriendGroup, "FriendGroup"),
			"delete-friend-group": routeFallible(csil.DecodeSocialDeleteFriendGroupRequest, svcs.Social.DeleteFriendGroup, csil.EncodeEmpty, "Empty"),
			"add-friend":          routeFallible(csil.DecodeAddFriendRequest, svcs.Social.AddFriend, csil.EncodeEmpty, "Empty"),
			"remove-friend":       routeFallible(csil.DecodeRemoveFriendRequest, svcs.Social.RemoveFriend, csil.EncodeEmpty, "Empty"),
			"resolve-user":        routeFallible(csil.DecodeSocialResolveUserRequest, svcs.Social.ResolveUser, csil.EncodeUserProfile, "UserProfile"),
		},
		"subscription": {
			"subscribe":          routeFallible(csil.DecodeTargetRef, svcs.Subscription.Subscribe, csil.EncodeSubscription, "Subscription"),
			"unsubscribe":        routeFallible(csil.DecodeTargetRef, svcs.Subscription.Unsubscribe, csil.EncodeEmpty, "Empty"),
			"set-muted":          routeFallible(csil.DecodeSetMutedRequest, svcs.Subscription.SetMuted, csil.EncodeSubscription, "Subscription"),
			"list-subscriptions": routeInfallible(csil.DecodeEmpty, svcs.Subscription.ListSubscriptions, csil.EncodeSubscriptionList, "SubscriptionList"),
		},
		"read": {
			"mark-read":      routeInfallible(csil.DecodeTargetRef, svcs.Read.MarkRead, csil.EncodeEmpty, "Empty"),
			"mark-unread":    routeInfallible(csil.DecodeTargetRef, svcs.Read.MarkUnread, csil.EncodeEmpty, "Empty"),
			"unread-summary": routeInfallible(csil.DecodeEmpty, svcs.Read.UnreadSummary, csil.EncodeUnreadSummary, "UnreadSummary"),
		},
		"notification": {
			"list-notifications":     routeInfallible(csil.DecodeListNotificationsRequest, svcs.Notification.ListNotifications, csil.EncodeNotificationPage, "NotificationPage"),
			"mark-notification-read": routeInfallible(csil.DecodeNotificationMarkNotificationReadRequest, svcs.Notification.MarkNotificationRead, csil.EncodeEmpty, "Empty"),
			"mark-all-read":          routeInfallible(csil.DecodeEmpty, svcs.Notification.MarkAllRead, csil.EncodeEmpty, "Empty"),
		},
		"integration": {
			"create-github-mapping": routeFallible(csil.DecodeCreateMappingRequest, svcs.Integration.CreateGithubMapping, csil.EncodeGithubMapping, "GithubMapping"),
			"list-github-mappings":  routeInfallible(csil.DecodeEmpty, svcs.Integration.ListGithubMappings, csil.EncodeMappingList, "MappingList"),
			"delete-github-mapping": routeFallible(csil.DecodeIntegrationDeleteGithubMappingRequest, svcs.Integration.DeleteGithubMapping, csil.EncodeEmpty, "Empty"),
			"add-trusted-domain":    routeFallible(csil.DecodeIntegrationAddTrustedDomainRequest, svcs.Integration.AddTrustedDomain, csil.EncodeEmpty, "Empty"),
			"remove-trusted-domain": routeFallible(csil.DecodeIntegrationRemoveTrustedDomainRequest, svcs.Integration.RemoveTrustedDomain, csil.EncodeEmpty, "Empty"),
			"list-trusted-domains":  routeInfallible(csil.DecodeEmpty, svcs.Integration.ListTrustedDomains, csil.EncodeDomainList, "DomainList"),
		},
	}
}

// dispatch resolves req against routes, returning a transport-level
// "unknown service/op" outcome if there's no match.
func dispatch(ctx context.Context, routes map[string]map[string]typedHandler, req *transport.RpcRequest) transport.HandlerOutcome {
	service := strings.ToLower(strings.TrimSuffix(req.Service, "Service"))
	ops, ok := routes[service]
	if !ok {
		return transport.Transport(transport.StatusUnknownServiceOrOp, "unknown service: "+req.Service)
	}
	handler, ok := ops[req.Op]
	if !ok {
		return transport.Transport(transport.StatusUnknownServiceOrOp, "unknown op: "+req.Service+"/"+req.Op)
	}
	return handler(ctx, req.Payload)
}
