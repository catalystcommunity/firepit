// Round-trip coverage: for one representative operation per firepit
// service, build a sample request/response value, encode it with the
// csilgen-generated codec (this package), wrap it in a CSIL-RPC envelope via
// the vendored transport library (api/internal/transport), and verify the
// envelope decodes back to byte-identical bytes and the payload decodes back
// to an equal value. This exercises exactly the seam PLANDOC.md task A2
// requires: generated types + codec on one side, the CSIL-RPC transport on
// the other, with no server/dispatch code in between (that's task B1).
package csil

import (
	"reflect"
	"testing"

	"github.com/catalystcommunity/firepit/api/internal/transport"
)

// rpcCase exercises one operation's request and response envelopes.
type rpcCase struct {
	service string
	op      string

	reqPayload   []byte
	reqDecodeEq  func(t *testing.T, payload []byte)
	respVariant  string
	respPayload  []byte
	respDecodeEq func(t *testing.T, payload []byte)
}

func TestRpcRoundTripPerService(t *testing.T) {
	cases := []rpcCase{
		{
			service:    "AuthService",
			op:         "begin-login",
			reqPayload: EncodeBeginLoginRequest(BeginLoginRequest{Domain: "todandlorna.com"}),
			reqDecodeEq: func(t *testing.T, payload []byte) {
				got, err := DecodeBeginLoginRequest(payload)
				if err != nil {
					t.Fatalf("decode: %v", err)
				}
				want := BeginLoginRequest{Domain: "todandlorna.com"}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("got %+v want %+v", got, want)
				}
			},
			respVariant: "BeginLoginResponse",
			respPayload: EncodeBeginLoginResponse(BeginLoginResponse{RedirectUrl: "https://linkkeys.todandlorna.com/login"}),
			respDecodeEq: func(t *testing.T, payload []byte) {
				got, err := DecodeBeginLoginResponse(payload)
				if err != nil {
					t.Fatalf("decode: %v", err)
				}
				want := BeginLoginResponse{RedirectUrl: "https://linkkeys.todandlorna.com/login"}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("got %+v want %+v", got, want)
				}
			},
		},
		{
			service:    "BoardService",
			op:         "list-boards",
			reqPayload: EncodeListBoardsRequest(ListBoardsRequest{}),
			reqDecodeEq: func(t *testing.T, payload []byte) {
				got, err := DecodeListBoardsRequest(payload)
				if err != nil {
					t.Fatalf("decode: %v", err)
				}
				if !reflect.DeepEqual(got, ListBoardsRequest{}) {
					t.Errorf("got %+v want zero value", got)
				}
			},
			respVariant: "BoardPage",
			respPayload: EncodeBoardPage(BoardPage{Boards: []Board{{Id: "01H000000000000000000BOARD", Slug: "firepit", Title: "Firepit"}}}),
			respDecodeEq: func(t *testing.T, payload []byte) {
				got, err := DecodeBoardPage(payload)
				if err != nil {
					t.Fatalf("decode: %v", err)
				}
				want := BoardPage{Boards: []Board{{Id: "01H000000000000000000BOARD", Slug: "firepit", Title: "Firepit"}}}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("got %+v want %+v", got, want)
				}
			},
		},
		{
			service:    "ThreadService",
			op:         "list-posts",
			reqPayload: EncodeListPostsRequest(ListPostsRequest{BoardId: "01H000000000000000000BOARD"}),
			reqDecodeEq: func(t *testing.T, payload []byte) {
				got, err := DecodeListPostsRequest(payload)
				if err != nil {
					t.Fatalf("decode: %v", err)
				}
				want := ListPostsRequest{BoardId: "01H000000000000000000BOARD"}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("got %+v want %+v", got, want)
				}
			},
			respVariant: "PostPage",
			respPayload: EncodePostPage(PostPage{Posts: []Post{{Id: "01H0000000000000000POST01", Title: "Hello firepit"}}}),
			respDecodeEq: func(t *testing.T, payload []byte) {
				got, err := DecodePostPage(payload)
				if err != nil {
					t.Fatalf("decode: %v", err)
				}
				want := PostPage{Posts: []Post{{Id: "01H0000000000000000POST01", Title: "Hello firepit"}}}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("got %+v want %+v", got, want)
				}
			},
		},
		{
			service:    "EndorsementService",
			op:         "list-endorsements",
			reqPayload: EncodeTargetRef(TargetRef{TargetType: "post", TargetId: "01H0000000000000000POST01"}),
			reqDecodeEq: func(t *testing.T, payload []byte) {
				got, err := DecodeTargetRef(payload)
				if err != nil {
					t.Fatalf("decode: %v", err)
				}
				want := TargetRef{TargetType: "post", TargetId: "01H0000000000000000POST01"}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("got %+v want %+v", got, want)
				}
			},
			respVariant: "EndorsementList",
			respPayload: EncodeEndorsementList(EndorsementList{Endorsements: []Endorsement{{Id: "01H00000000000000ENDORSE1", UserId: "01H000000000000000000USER"}}}),
			respDecodeEq: func(t *testing.T, payload []byte) {
				got, err := DecodeEndorsementList(payload)
				if err != nil {
					t.Fatalf("decode: %v", err)
				}
				want := EndorsementList{Endorsements: []Endorsement{{Id: "01H00000000000000ENDORSE1", UserId: "01H000000000000000000USER"}}}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("got %+v want %+v", got, want)
				}
			},
		},
		{
			service:    "SettingsService",
			op:         "get-settings",
			reqPayload: EncodeEmpty(Empty{}),
			reqDecodeEq: func(t *testing.T, payload []byte) {
				got, err := DecodeEmpty(payload)
				if err != nil {
					t.Fatalf("decode: %v", err)
				}
				if !reflect.DeepEqual(got, Empty{}) {
					t.Errorf("got %+v want zero value", got)
				}
			},
			respVariant: "UserSettings",
			respPayload: EncodeUserSettings(UserSettings{MentionPolicy: "subscribed", NotifyOnEndorse: true}),
			respDecodeEq: func(t *testing.T, payload []byte) {
				got, err := DecodeUserSettings(payload)
				if err != nil {
					t.Fatalf("decode: %v", err)
				}
				want := UserSettings{MentionPolicy: "subscribed", NotifyOnEndorse: true}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("got %+v want %+v", got, want)
				}
			},
		},
		{
			service:    "SocialService",
			op:         "list-friend-groups",
			reqPayload: EncodeEmpty(Empty{}),
			reqDecodeEq: func(t *testing.T, payload []byte) {
				got, err := DecodeEmpty(payload)
				if err != nil {
					t.Fatalf("decode: %v", err)
				}
				if !reflect.DeepEqual(got, Empty{}) {
					t.Errorf("got %+v want zero value", got)
				}
			},
			respVariant: "FriendGroupList",
			respPayload: EncodeFriendGroupList(FriendGroupList{Groups: []FriendGroup{{Id: "01H0000000000000000GROUP1", Name: "close friends", Members: []UserID{}}}}),
			respDecodeEq: func(t *testing.T, payload []byte) {
				got, err := DecodeFriendGroupList(payload)
				if err != nil {
					t.Fatalf("decode: %v", err)
				}
				want := FriendGroupList{Groups: []FriendGroup{{Id: "01H0000000000000000GROUP1", Name: "close friends", Members: []UserID{}}}}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("got %+v want %+v", got, want)
				}
			},
		},
		{
			service:    "SubscriptionService",
			op:         "list-subscriptions",
			reqPayload: EncodeEmpty(Empty{}),
			reqDecodeEq: func(t *testing.T, payload []byte) {
				got, err := DecodeEmpty(payload)
				if err != nil {
					t.Fatalf("decode: %v", err)
				}
				if !reflect.DeepEqual(got, Empty{}) {
					t.Errorf("got %+v want zero value", got)
				}
			},
			respVariant: "SubscriptionList",
			respPayload: EncodeSubscriptionList(SubscriptionList{Subscriptions: []Subscription{{Id: "01H0000000000000000SUBSC1", TargetType: "board", TargetId: "01H000000000000000000BOARD"}}}),
			respDecodeEq: func(t *testing.T, payload []byte) {
				got, err := DecodeSubscriptionList(payload)
				if err != nil {
					t.Fatalf("decode: %v", err)
				}
				want := SubscriptionList{Subscriptions: []Subscription{{Id: "01H0000000000000000SUBSC1", TargetType: "board", TargetId: "01H000000000000000000BOARD"}}}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("got %+v want %+v", got, want)
				}
			},
		},
		{
			service:    "ReadService",
			op:         "unread-summary",
			reqPayload: EncodeEmpty(Empty{}),
			reqDecodeEq: func(t *testing.T, payload []byte) {
				got, err := DecodeEmpty(payload)
				if err != nil {
					t.Fatalf("decode: %v", err)
				}
				if !reflect.DeepEqual(got, Empty{}) {
					t.Errorf("got %+v want zero value", got)
				}
			},
			respVariant: "UnreadSummary",
			respPayload: EncodeUnreadSummary(UnreadSummary{Boards: []BoardUnread{{BoardId: "01H000000000000000000BOARD", UnreadCount: 3, UnreadPostIds: []PostID{}}}}),
			respDecodeEq: func(t *testing.T, payload []byte) {
				got, err := DecodeUnreadSummary(payload)
				if err != nil {
					t.Fatalf("decode: %v", err)
				}
				want := UnreadSummary{Boards: []BoardUnread{{BoardId: "01H000000000000000000BOARD", UnreadCount: 3, UnreadPostIds: []PostID{}}}}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("got %+v want %+v", got, want)
				}
			},
		},
		{
			service:    "NotificationService",
			op:         "list-notifications",
			reqPayload: EncodeListNotificationsRequest(ListNotificationsRequest{}),
			reqDecodeEq: func(t *testing.T, payload []byte) {
				got, err := DecodeListNotificationsRequest(payload)
				if err != nil {
					t.Fatalf("decode: %v", err)
				}
				if !reflect.DeepEqual(got, ListNotificationsRequest{}) {
					t.Errorf("got %+v want zero value", got)
				}
			},
			respVariant: "NotificationPage",
			respPayload: EncodeNotificationPage(NotificationPage{Notifications: []Notification{{Id: "01H00000000NOTIFICATION01", Event: "mention"}}}),
			respDecodeEq: func(t *testing.T, payload []byte) {
				got, err := DecodeNotificationPage(payload)
				if err != nil {
					t.Fatalf("decode: %v", err)
				}
				want := NotificationPage{Notifications: []Notification{{Id: "01H00000000NOTIFICATION01", Event: "mention"}}}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("got %+v want %+v", got, want)
				}
			},
		},
		{
			service:    "IntegrationService",
			op:         "list-github-mappings",
			reqPayload: EncodeEmpty(Empty{}),
			reqDecodeEq: func(t *testing.T, payload []byte) {
				got, err := DecodeEmpty(payload)
				if err != nil {
					t.Fatalf("decode: %v", err)
				}
				if !reflect.DeepEqual(got, Empty{}) {
					t.Errorf("got %+v want zero value", got)
				}
			},
			respVariant: "MappingList",
			respPayload: EncodeMappingList(MappingList{Mappings: []GithubMapping{{Id: "01H00000000000000MAPPING1", Repo: "catalystcommunity/firepit", Events: []string{}}}}),
			respDecodeEq: func(t *testing.T, payload []byte) {
				got, err := DecodeMappingList(payload)
				if err != nil {
					t.Fatalf("decode: %v", err)
				}
				want := MappingList{Mappings: []GithubMapping{{Id: "01H00000000000000MAPPING1", Repo: "catalystcommunity/firepit", Events: []string{}}}}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("got %+v want %+v", got, want)
				}
			},
		},
	}

	for i, tc := range cases {
		tc := tc
		t.Run(tc.service+"/"+tc.op, func(t *testing.T) {
			// Request envelope: client -> server, one-in-flight (HTTP), no id.
			req := transport.NewRpcRequest(tc.service, tc.op, tc.reqPayload)
			reqBytes, err := req.Encode()
			if err != nil {
				t.Fatalf("encode request envelope: %v", err)
			}
			decodedReq, err := transport.DecodeRpcRequest(reqBytes)
			if err != nil {
				t.Fatalf("decode request envelope: %v", err)
			}
			if decodedReq.Service != tc.service || decodedReq.Op != tc.op {
				t.Fatalf("envelope service/op mismatch: got (%s, %s) want (%s, %s)",
					decodedReq.Service, decodedReq.Op, tc.service, tc.op)
			}
			if !reflect.DeepEqual(decodedReq.Payload, tc.reqPayload) {
				t.Fatalf("request payload mismatch after envelope round-trip")
			}
			tc.reqDecodeEq(t, decodedReq.Payload)

			// Response envelope: server -> client, status ok, multiplexed id
			// echoed (exercises the `id` field too, which HTTP may omit but a
			// multiplexed carrier requires).
			id := uint64(i + 1)
			resp := transport.NewRpcResponseOk(tc.respVariant, tc.respPayload).WithID(&id)
			respBytes, err := resp.Encode()
			if err != nil {
				t.Fatalf("encode response envelope: %v", err)
			}
			decodedResp, err := transport.DecodeRpcResponse(respBytes)
			if err != nil {
				t.Fatalf("decode response envelope: %v", err)
			}
			if decodedResp.ID == nil || *decodedResp.ID != id {
				t.Fatalf("response id mismatch: got %v want %d", decodedResp.ID, id)
			}
			if decodedResp.Variant == nil || *decodedResp.Variant != tc.respVariant {
				t.Fatalf("response variant mismatch: got %v want %s", decodedResp.Variant, tc.respVariant)
			}
			if err := decodedResp.AsTransportError(); err != nil {
				t.Fatalf("unexpected transport error: %v", err)
			}
			if !reflect.DeepEqual(decodedResp.Payload, tc.respPayload) {
				t.Fatalf("response payload mismatch after envelope round-trip")
			}
			tc.respDecodeEq(t, decodedResp.Payload)
		})
	}
}

// TestRpcTransportErrorResponse verifies a non-zero transport status carries
// no typed payload and surfaces as a transport-level error, never mistaken
// for an application error riding in the payload (csil-rpc-transport.md §5,
// "transport-error response").
func TestRpcTransportErrorResponse(t *testing.T) {
	resp := transport.NewRpcResponseTransportError(transport.StatusUnknownServiceOrOp, "unknown op")
	b, err := resp.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := transport.DecodeRpcResponse(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded.Payload) != 0 {
		t.Fatalf("expected empty payload on transport error, got %d bytes", len(decoded.Payload))
	}
	if err := decoded.AsTransportError(); err == nil {
		t.Fatal("expected a transport error, got nil")
	}
}
