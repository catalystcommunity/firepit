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
			"begin-login": routeFallible(csil.DecodeAuthBeginLoginRequest, svcs.Auth.BeginLogin, csil.EncodeAuthBeginLoginResponse, "BeginLoginResponse"),
			"logout":      routeInfallible(csil.DecodeAuthLogoutRequest, svcs.Auth.Logout, csil.EncodeAuthLogoutResponse, "Empty"),
			"whoami":      routeFallible(csil.DecodeAuthWhoamiRequest, svcs.Auth.Whoami, csil.EncodeAuthWhoamiResponse, "UserProfile"),
		},
		"board": {
			"list-boards":         routeInfallible(csil.DecodeBoardListBoardsRequest, svcs.Board.ListBoards, csil.EncodeBoardListBoardsResponse, "BoardPage"),
			"get-board":           routeFallible(csil.DecodeBoardGetBoardRequest, svcs.Board.GetBoard, csil.EncodeBoardGetBoardResponse, "Board"),
			"create-board":        routeFallible(csil.DecodeBoardCreateBoardRequest, svcs.Board.CreateBoard, csil.EncodeBoardCreateBoardResponse, "Board"),
			"update-board":        routeFallible(csil.DecodeBoardUpdateBoardRequest, svcs.Board.UpdateBoard, csil.EncodeBoardUpdateBoardResponse, "Board"),
			"archive-board":       routeFallible(csil.DecodeBoardArchiveBoardRequest, svcs.Board.ArchiveBoard, csil.EncodeBoardArchiveBoardResponse, "Empty"),
			"set-board-member":    routeFallible(csil.DecodeBoardSetBoardMemberRequest, svcs.Board.SetBoardMember, csil.EncodeBoardSetBoardMemberResponse, "Empty"),
			"remove-board-member": routeFallible(csil.DecodeBoardRemoveBoardMemberRequest, svcs.Board.RemoveBoardMember, csil.EncodeBoardRemoveBoardMemberResponse, "Empty"),
		},
		"thread": {
			"list-posts":     routeInfallible(csil.DecodeThreadListPostsRequest, svcs.Thread.ListPosts, csil.EncodeThreadListPostsResponse, "PostPage"),
			"get-thread":     routeInfallible(csil.DecodeThreadGetThreadRequest, svcs.Thread.GetThread, csil.EncodeThreadGetThreadResponse, "Thread"),
			"create-post":    routeFallible(csil.DecodeThreadCreatePostRequest, svcs.Thread.CreatePost, csil.EncodeThreadCreatePostResponse, "Post"),
			"create-comment": routeFallible(csil.DecodeThreadCreateCommentRequest, svcs.Thread.CreateComment, csil.EncodeThreadCreateCommentResponse, "Comment"),
			"edit-post":      routeFallible(csil.DecodeThreadEditPostRequest, svcs.Thread.EditPost, csil.EncodeThreadEditPostResponse, "Post"),
			"edit-comment":   routeFallible(csil.DecodeThreadEditCommentRequest, svcs.Thread.EditComment, csil.EncodeThreadEditCommentResponse, "Comment"),
			"list-revisions": routeInfallible(csil.DecodeThreadListRevisionsRequest, svcs.Thread.ListRevisions, csil.EncodeThreadListRevisionsResponse, "RevisionList"),
			"delete-post":    routeFallible(csil.DecodeThreadDeletePostRequest, svcs.Thread.DeletePost, csil.EncodeThreadDeletePostResponse, "Empty"),
			"delete-comment": routeFallible(csil.DecodeThreadDeleteCommentRequest, svcs.Thread.DeleteComment, csil.EncodeThreadDeleteCommentResponse, "Empty"),
		},
		"endorsement": {
			"endorse":           routeFallible(csil.DecodeEndorsementEndorseRequest, svcs.Endorsement.Endorse, csil.EncodeEndorsementEndorseResponse, "Endorsement"),
			"retract":           routeFallible(csil.DecodeEndorsementRetractRequest, svcs.Endorsement.Retract, csil.EncodeEndorsementRetractResponse, "Empty"),
			"list-endorsements": routeInfallible(csil.DecodeEndorsementListEndorsementsRequest, svcs.Endorsement.ListEndorsements, csil.EncodeEndorsementListEndorsementsResponse, "EndorsementList"),
		},
		"settings": {
			"get-settings":        routeInfallible(csil.DecodeSettingsGetSettingsRequest, svcs.Settings.GetSettings, csil.EncodeSettingsGetSettingsResponse, "UserSettings"),
			"update-settings":     routeFallible(csil.DecodeSettingsUpdateSettingsRequest, svcs.Settings.UpdateSettings, csil.EncodeSettingsUpdateSettingsResponse, "UserSettings"),
			"list-mention-grants": routeInfallible(csil.DecodeSettingsListMentionGrantsRequest, svcs.Settings.ListMentionGrants, csil.EncodeSettingsListMentionGrantsResponse, "MentionGrantList"),
			"grant-mention":       routeFallible(csil.DecodeSettingsGrantMentionRequest, svcs.Settings.GrantMention, csil.EncodeSettingsGrantMentionResponse, "Empty"),
			"revoke-mention":      routeFallible(csil.DecodeSettingsRevokeMentionRequest, svcs.Settings.RevokeMention, csil.EncodeSettingsRevokeMentionResponse, "Empty"),
		},
		"social": {
			"list-friend-groups":  routeInfallible(csil.DecodeSocialListFriendGroupsRequest, svcs.Social.ListFriendGroups, csil.EncodeSocialListFriendGroupsResponse, "FriendGroupList"),
			"create-friend-group": routeFallible(csil.DecodeSocialCreateFriendGroupRequest, svcs.Social.CreateFriendGroup, csil.EncodeSocialCreateFriendGroupResponse, "FriendGroup"),
			"delete-friend-group": routeFallible(csil.DecodeSocialDeleteFriendGroupRequest, svcs.Social.DeleteFriendGroup, csil.EncodeSocialDeleteFriendGroupResponse, "Empty"),
			"add-friend":          routeFallible(csil.DecodeSocialAddFriendRequest, svcs.Social.AddFriend, csil.EncodeSocialAddFriendResponse, "Empty"),
			"remove-friend":       routeFallible(csil.DecodeSocialRemoveFriendRequest, svcs.Social.RemoveFriend, csil.EncodeSocialRemoveFriendResponse, "Empty"),
		},
		"subscription": {
			"subscribe":           routeFallible(csil.DecodeSubscriptionSubscribeRequest, svcs.Subscription.Subscribe, csil.EncodeSubscriptionSubscribeResponse, "Subscription"),
			"unsubscribe":         routeFallible(csil.DecodeSubscriptionUnsubscribeRequest, svcs.Subscription.Unsubscribe, csil.EncodeSubscriptionUnsubscribeResponse, "Empty"),
			"set-muted":           routeFallible(csil.DecodeSubscriptionSetMutedRequest, svcs.Subscription.SetMuted, csil.EncodeSubscriptionSetMutedResponse, "Subscription"),
			"list-subscriptions":  routeInfallible(csil.DecodeSubscriptionListSubscriptionsRequest, svcs.Subscription.ListSubscriptions, csil.EncodeSubscriptionListSubscriptionsResponse, "SubscriptionList"),
		},
		"read": {
			"mark-read":      routeInfallible(csil.DecodeReadMarkReadRequest, svcs.Read.MarkRead, csil.EncodeReadMarkReadResponse, "Empty"),
			"mark-unread":    routeInfallible(csil.DecodeReadMarkUnreadRequest, svcs.Read.MarkUnread, csil.EncodeReadMarkUnreadResponse, "Empty"),
			"unread-summary": routeInfallible(csil.DecodeReadUnreadSummaryRequest, svcs.Read.UnreadSummary, csil.EncodeReadUnreadSummaryResponse, "UnreadSummary"),
		},
		"notification": {
			"list-notifications":     routeInfallible(csil.DecodeNotificationListNotificationsRequest, svcs.Notification.ListNotifications, csil.EncodeNotificationListNotificationsResponse, "NotificationPage"),
			"mark-notification-read": routeInfallible(csil.DecodeNotificationMarkNotificationReadRequest, svcs.Notification.MarkNotificationRead, csil.EncodeNotificationMarkNotificationReadResponse, "Empty"),
			"mark-all-read":          routeInfallible(csil.DecodeNotificationMarkAllReadRequest, svcs.Notification.MarkAllRead, csil.EncodeNotificationMarkAllReadResponse, "Empty"),
		},
		"integration": {
			"create-github-mapping": routeFallible(csil.DecodeIntegrationCreateGithubMappingRequest, svcs.Integration.CreateGithubMapping, csil.EncodeIntegrationCreateGithubMappingResponse, "GithubMapping"),
			"list-github-mappings":  routeInfallible(csil.DecodeIntegrationListGithubMappingsRequest, svcs.Integration.ListGithubMappings, csil.EncodeIntegrationListGithubMappingsResponse, "MappingList"),
			"delete-github-mapping": routeFallible(csil.DecodeIntegrationDeleteGithubMappingRequest, svcs.Integration.DeleteGithubMapping, csil.EncodeIntegrationDeleteGithubMappingResponse, "Empty"),
			"add-trusted-domain":    routeFallible(csil.DecodeIntegrationAddTrustedDomainRequest, svcs.Integration.AddTrustedDomain, csil.EncodeIntegrationAddTrustedDomainResponse, "Empty"),
			"remove-trusted-domain": routeFallible(csil.DecodeIntegrationRemoveTrustedDomainRequest, svcs.Integration.RemoveTrustedDomain, csil.EncodeIntegrationRemoveTrustedDomainResponse, "Empty"),
			"list-trusted-domains":  routeInfallible(csil.DecodeIntegrationListTrustedDomainsRequest, svcs.Integration.ListTrustedDomains, csil.EncodeIntegrationListTrustedDomainsResponse, "DomainList"),
		},
	}
}

// dispatch resolves req against routes, returning a transport-level
// "unknown service/op" outcome if there's no match.
func dispatch(ctx context.Context, routes map[string]map[string]typedHandler, req *transport.RpcRequest) transport.HandlerOutcome {
	ops, ok := routes[req.Service]
	if !ok {
		return transport.Transport(transport.StatusUnknownServiceOrOp, "unknown service: "+req.Service)
	}
	handler, ok := ops[req.Op]
	if !ok {
		return transport.Transport(transport.StatusUnknownServiceOrOp, "unknown op: "+req.Service+"/"+req.Op)
	}
	return handler(ctx, req.Payload)
}
