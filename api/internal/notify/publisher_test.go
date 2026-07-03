package notify

import "testing"

// TestMentionGateAllows exercises the pure mention_policy decision matrix
// (PLANDOC.md §4, §9 decision 4), independent of any DB fetch — the
// DB-dependent parts (loading the policy, checking mention_grants, checking
// subscribed-context) live in mentionAllowed and are covered by the
// integration suite (api/internal/store's notification fan-out tests),
// since they need real subscriptions/settings/grants rows.
func TestMentionGateAllows(t *testing.T) {
	cases := []struct {
		name             string
		policy           string
		inScopeOrGranted bool
		want             bool
	}{
		{"everyone notifies regardless of scope/grant", "everyone", false, true},
		{"everyone notifies even if also in scope", "everyone", true, true},
		{"nobody never notifies even if granted", "nobody", true, false},
		{"nobody never notifies", "nobody", false, false},
		{"authorized notifies when granted", "authorized", true, true},
		{"authorized denies without a grant", "authorized", false, false},
		{"subscribed notifies when in scope or granted", "subscribed", true, true},
		{"subscribed denies otherwise", "subscribed", false, false},
		{"unknown policy fails closed", "some-future-policy", true, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mentionGateAllows(tc.policy, tc.inScopeOrGranted)
			if got != tc.want {
				t.Errorf("mentionGateAllows(%q, %v) = %v, want %v", tc.policy, tc.inScopeOrGranted, got, tc.want)
			}
		})
	}
}

func TestContentTarget(t *testing.T) {
	postEvent := Event{Kind: KindContentCreated, PostID: "post-1"}
	targetType, targetID, event := contentTarget(postEvent, "comment-event", "post-event")
	if targetType != "post" || targetID != "post-1" || event != "post-event" {
		t.Errorf("contentTarget(post event) = (%q, %q, %q), want (post, post-1, post-event)", targetType, targetID, event)
	}

	commentEvent := Event{Kind: KindContentCreated, PostID: "post-1", CommentID: "comment-1"}
	targetType, targetID, event = contentTarget(commentEvent, "comment-event", "post-event")
	if targetType != "comment" || targetID != "comment-1" || event != "comment-event" {
		t.Errorf("contentTarget(comment event) = (%q, %q, %q), want (comment, comment-1, comment-event)", targetType, targetID, event)
	}
}

func TestStrongestPerUser(t *testing.T) {
	rows := []subscriberCandidate{
		{SubscriptionID: "board-sub", UserID: "u1", Muted: false, Priority: 0},
		{SubscriptionID: "comment-sub", UserID: "u1", Muted: true, Priority: 5},
		{SubscriptionID: "post-sub", UserID: "u2", Muted: false, Priority: 1},
	}
	winners := strongestPerUser(rows)

	if got := winners["u1"]; got.SubscriptionID != "comment-sub" || !got.Muted {
		t.Errorf("u1 winner = %+v, want the priority-5 muted comment-sub row", got)
	}
	if got := winners["u2"]; got.SubscriptionID != "post-sub" {
		t.Errorf("u2 winner = %+v, want post-sub", got)
	}
	if len(winners) != 2 {
		t.Errorf("len(winners) = %d, want 2", len(winners))
	}
}
