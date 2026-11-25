// Package server provides the Firepit HTTP server. This file maps CSIL
// service and operation names to the generated service interfaces.
// Generated clients send the service and operation names from the schema.
// Dispatch also accepts the short service names used by earlier clients.
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

// Services bundles one implementation per generated CSIL service interface.
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

// route decodes a request and encodes its service result as an RPC reply.
func route[Req any, Resp any](
	decode func([]byte) (Req, error),
	fn func(context.Context, Req) (Resp, error),
	encodeReply func(Resp, error) (string, []byte, error),
) typedHandler {
	return func(ctx context.Context, payload []byte) transport.HandlerOutcome {
		req, err := decode(payload)
		if err != nil {
			return transport.Transport(transport.StatusMalformedEnvelope, "decode request: "+err.Error())
		}
		variant, body, err := encodeReply(fn(ctx, req))
		if err != nil {
			log.WithError(err).Error("CSIL service failed to produce a reply")
			return transport.Transport(transport.StatusInternal, "internal error")
		}
		return transport.Reply(variant, body)
	}
}

// fallible passes declared application errors to the generated reply encoder.
func fallible[T any](encode func(T, error) (string, []byte, error)) func(T, error) (string, []byte, error) {
	return func(value T, err error) (string, []byte, error) {
		var appErr *csilservices.AppError
		if errors.As(err, &appErr) {
			err = &csil.RPCApplicationError[csil.ServiceError]{Value: appErr.ServiceError()}
		}
		return encode(value, err)
	}
}

func single[T any](variant string, encode func(T) ([]byte, error)) func(T, error) (string, []byte, error) {
	return func(value T, err error) (string, []byte, error) {
		if err != nil {
			return "", nil, err
		}
		payload, err := encode(value)
		return variant, payload, err
	}
}

// buildRoutes constructs the full (service, op) routing table from svcs. One
// row per operation declared in csil/firepit.csil, in service-declaration
// order.
func buildRoutes(svcs Services) map[string]map[string]typedHandler {
	return map[string]map[string]typedHandler{
		"auth": {
			"begin-login": route(csil.DecodeBeginLoginRequest, svcs.Auth.BeginLogin, fallible(csil.EncodeAuthBeginLoginReply)),
			"logout":      route(csil.DecodeEmpty, svcs.Auth.Logout, single("Empty", csil.EncodeEmpty)),
			"whoami":      route(csil.DecodeEmpty, svcs.Auth.Whoami, fallible(csil.EncodeAuthWhoamiReply)),
		},
		"board": {
			"list-boards":         route(csil.DecodeListBoardsRequest, svcs.Board.ListBoards, single("BoardPage", csil.EncodeBoardPage)),
			"get-board":           route(csil.DecodeBoardGetBoardRequest, svcs.Board.GetBoard, fallible(csil.EncodeBoardGetBoardReply)),
			"create-board":        route(csil.DecodeCreateBoardRequest, svcs.Board.CreateBoard, fallible(csil.EncodeBoardCreateBoardReply)),
			"update-board":        route(csil.DecodeUpdateBoardRequest, svcs.Board.UpdateBoard, fallible(csil.EncodeBoardUpdateBoardReply)),
			"archive-board":       route(csil.DecodeBoardArchiveBoardRequest, svcs.Board.ArchiveBoard, fallible(csil.EncodeBoardArchiveBoardReply)),
			"set-board-member":    route(csil.DecodeSetBoardMemberRequest, svcs.Board.SetBoardMember, fallible(csil.EncodeBoardSetBoardMemberReply)),
			"remove-board-member": route(csil.DecodeRemoveBoardMemberRequest, svcs.Board.RemoveBoardMember, fallible(csil.EncodeBoardRemoveBoardMemberReply)),
		},
		"category": {
			"list-board-categories": route(csil.DecodeCategoryListBoardCategoriesRequest, svcs.Category.ListBoardCategories, single("CategoryList", csil.EncodeCategoryList)),
			"create-category":       route(csil.DecodeCreateCategoryRequest, svcs.Category.CreateCategory, fallible(csil.EncodeCategoryCreateCategoryReply)),
			"update-category":       route(csil.DecodeUpdateCategoryRequest, svcs.Category.UpdateCategory, fallible(csil.EncodeCategoryUpdateCategoryReply)),
			"delete-category":       route(csil.DecodeDeleteCategoryRequest, svcs.Category.DeleteCategory, fallible(csil.EncodeCategoryDeleteCategoryReply)),
			"list-categories":       route(csil.DecodeEmpty, svcs.Category.ListCategories, single("CategoryList", csil.EncodeCategoryList)),
		},
		"thread": {
			"list-posts":     route(csil.DecodeListPostsRequest, svcs.Thread.ListPosts, single("PostPage", csil.EncodePostPage)),
			"get-thread":     route(csil.DecodeGetThreadRequest, svcs.Thread.GetThread, single("Thread", csil.EncodeThread)),
			"create-post":    route(csil.DecodeCreatePostRequest, svcs.Thread.CreatePost, fallible(csil.EncodeThreadCreatePostReply)),
			"create-comment": route(csil.DecodeCreateCommentRequest, svcs.Thread.CreateComment, fallible(csil.EncodeThreadCreateCommentReply)),
			"edit-post":      route(csil.DecodeEditPostRequest, svcs.Thread.EditPost, fallible(csil.EncodeThreadEditPostReply)),
			"edit-comment":   route(csil.DecodeEditCommentRequest, svcs.Thread.EditComment, fallible(csil.EncodeThreadEditCommentReply)),
			"list-revisions": route(csil.DecodeTargetRef, svcs.Thread.ListRevisions, single("RevisionList", csil.EncodeRevisionList)),
			"delete-post":    route(csil.DecodeThreadDeletePostRequest, svcs.Thread.DeletePost, fallible(csil.EncodeThreadDeletePostReply)),
			"delete-comment": route(csil.DecodeThreadDeleteCommentRequest, svcs.Thread.DeleteComment, fallible(csil.EncodeThreadDeleteCommentReply)),
		},
		"endorsement": {
			"endorse":           route(csil.DecodeEndorseRequest, svcs.Endorsement.Endorse, fallible(csil.EncodeEndorsementEndorseReply)),
			"retract":           route(csil.DecodeEndorseRequest, svcs.Endorsement.Retract, fallible(csil.EncodeEndorsementRetractReply)),
			"list-endorsements": route(csil.DecodeTargetRef, svcs.Endorsement.ListEndorsements, single("EndorsementList", csil.EncodeEndorsementList)),
		},
		"settings": {
			"get-settings":        route(csil.DecodeEmpty, svcs.Settings.GetSettings, single("UserSettings", csil.EncodeUserSettings)),
			"update-settings":     route(csil.DecodeUpdateSettingsRequest, svcs.Settings.UpdateSettings, fallible(csil.EncodeSettingsUpdateSettingsReply)),
			"list-mention-grants": route(csil.DecodeEmpty, svcs.Settings.ListMentionGrants, single("MentionGrantList", csil.EncodeMentionGrantList)),
			"grant-mention":       route(csil.DecodeSettingsGrantMentionRequest, svcs.Settings.GrantMention, fallible(csil.EncodeSettingsGrantMentionReply)),
			"revoke-mention":      route(csil.DecodeSettingsRevokeMentionRequest, svcs.Settings.RevokeMention, fallible(csil.EncodeSettingsRevokeMentionReply)),
		},
		"social": {
			"list-friend-groups":  route(csil.DecodeEmpty, svcs.Social.ListFriendGroups, single("FriendGroupList", csil.EncodeFriendGroupList)),
			"create-friend-group": route(csil.DecodeCreateFriendGroupRequest, svcs.Social.CreateFriendGroup, fallible(csil.EncodeSocialCreateFriendGroupReply)),
			"delete-friend-group": route(csil.DecodeSocialDeleteFriendGroupRequest, svcs.Social.DeleteFriendGroup, fallible(csil.EncodeSocialDeleteFriendGroupReply)),
			"add-friend":          route(csil.DecodeAddFriendRequest, svcs.Social.AddFriend, fallible(csil.EncodeSocialAddFriendReply)),
			"remove-friend":       route(csil.DecodeRemoveFriendRequest, svcs.Social.RemoveFriend, fallible(csil.EncodeSocialRemoveFriendReply)),
			"resolve-user":        route(csil.DecodeSocialResolveUserRequest, svcs.Social.ResolveUser, fallible(csil.EncodeSocialResolveUserReply)),
		},
		"subscription": {
			"subscribe":          route(csil.DecodeTargetRef, svcs.Subscription.Subscribe, fallible(csil.EncodeSubscriptionSubscribeReply)),
			"unsubscribe":        route(csil.DecodeTargetRef, svcs.Subscription.Unsubscribe, fallible(csil.EncodeSubscriptionUnsubscribeReply)),
			"set-muted":          route(csil.DecodeSetMutedRequest, svcs.Subscription.SetMuted, fallible(csil.EncodeSubscriptionSetMutedReply)),
			"list-subscriptions": route(csil.DecodeEmpty, svcs.Subscription.ListSubscriptions, single("SubscriptionList", csil.EncodeSubscriptionList)),
		},
		"read": {
			"mark-read":      route(csil.DecodeTargetRef, svcs.Read.MarkRead, single("Empty", csil.EncodeEmpty)),
			"mark-unread":    route(csil.DecodeTargetRef, svcs.Read.MarkUnread, single("Empty", csil.EncodeEmpty)),
			"unread-summary": route(csil.DecodeEmpty, svcs.Read.UnreadSummary, single("UnreadSummary", csil.EncodeUnreadSummary)),
		},
		"notification": {
			"list-notifications":     route(csil.DecodeListNotificationsRequest, svcs.Notification.ListNotifications, single("NotificationPage", csil.EncodeNotificationPage)),
			"mark-notification-read": route(csil.DecodeNotificationMarkNotificationReadRequest, svcs.Notification.MarkNotificationRead, single("Empty", csil.EncodeEmpty)),
			"mark-all-read":          route(csil.DecodeEmpty, svcs.Notification.MarkAllRead, single("Empty", csil.EncodeEmpty)),
		},
		"integration": {
			"create-github-mapping": route(csil.DecodeCreateMappingRequest, svcs.Integration.CreateGithubMapping, fallible(csil.EncodeIntegrationCreateGithubMappingReply)),
			"list-github-mappings":  route(csil.DecodeEmpty, svcs.Integration.ListGithubMappings, single("MappingList", csil.EncodeMappingList)),
			"delete-github-mapping": route(csil.DecodeIntegrationDeleteGithubMappingRequest, svcs.Integration.DeleteGithubMapping, fallible(csil.EncodeIntegrationDeleteGithubMappingReply)),
			"add-trusted-domain":    route(csil.DecodeIntegrationAddTrustedDomainRequest, svcs.Integration.AddTrustedDomain, fallible(csil.EncodeIntegrationAddTrustedDomainReply)),
			"remove-trusted-domain": route(csil.DecodeIntegrationRemoveTrustedDomainRequest, svcs.Integration.RemoveTrustedDomain, fallible(csil.EncodeIntegrationRemoveTrustedDomainReply)),
			"list-trusted-domains":  route(csil.DecodeEmpty, svcs.Integration.ListTrustedDomains, single("DomainList", csil.EncodeDomainList)),
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
