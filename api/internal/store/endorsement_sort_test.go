package store

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestSortEndorsementViews exercises the ordering comparator in isolation
// (no database) against the matrix PLANDOC.md §4/§7 (B5 acceptance)
// calls out explicitly: friend vs. board-role vs. trusted-domain
// reputation vs. plain ("nobody"), plus the created_at tiebreak within a
// tier and the anonymous-viewer (no friend tier) case.
func TestSortEndorsementViews(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	at := func(i int) time.Time { return t0.Add(time.Duration(i) * time.Minute) }

	view := func(id string, friend bool, role string, trusted int, createdAt time.Time) EndorsementView {
		return EndorsementView{
			Endorsement:  Endorsement{ID: id, CreatedAt: createdAt},
			RoleBadge:    role,
			IsFriend:     friend,
			TrustedCount: trusted,
		}
	}

	t.Run("friend beats maintainer beats trusted-domain rep beats plain", func(t *testing.T) {
		plain := view("plain", false, "", 0, at(0))
		trusted := view("trusted", false, "", 5, at(1))
		maintainer := view("maintainer", false, "maintainer", 0, at(2))
		friend := view("friend", true, "", 0, at(3))

		views := []EndorsementView{plain, trusted, maintainer, friend}
		sortEndorsementViews(views)

		var order []string
		for _, v := range views {
			order = append(order, v.Endorsement.ID)
		}
		assert.Equal(t, []string{"friend", "maintainer", "trusted", "plain"}, order)
	})

	t.Run("moderator role ranks with maintainer, both above trusted-domain count", func(t *testing.T) {
		moderator := view("moderator", false, "moderator", 100, at(0))
		trusted := view("trusted", false, "", 5, at(1))

		views := []EndorsementView{trusted, moderator}
		sortEndorsementViews(views)

		assert.Equal(t, "moderator", views[0].Endorsement.ID, "a board role outranks any trusted-domain count")
		assert.Equal(t, "trusted", views[1].Endorsement.ID)
	})

	t.Run("higher trusted-domain count ranks above a lower one", func(t *testing.T) {
		low := view("low", false, "", 1, at(0))
		high := view("high", false, "", 9, at(1))

		views := []EndorsementView{low, high}
		sortEndorsementViews(views)

		assert.Equal(t, []string{"high", "low"}, []string{views[0].Endorsement.ID, views[1].Endorsement.ID})
	})

	t.Run("stable tiebreak by endorsement created_at within an equal tier", func(t *testing.T) {
		earlier := view("earlier", false, "", 0, at(0))
		later := view("later", false, "", 0, at(1))

		// Feed them in reverse to prove the sort — not incidental input
		// order — produces the created_at ordering.
		views := []EndorsementView{later, earlier}
		sortEndorsementViews(views)

		assert.Equal(t, []string{"earlier", "later"}, []string{views[0].Endorsement.ID, views[1].Endorsement.ID})
	})

	t.Run("anonymous viewer (no friend data attached) gets reputation-only ordering", func(t *testing.T) {
		// A nil viewerID never produces IsFriend=true anywhere upstream, so
		// this is just the reputation tiers with every IsFriend false —
		// confirm that degrades to the same order as the friend-less cases
		// above rather than some special-cased anonymous path.
		maintainer := view("maintainer", false, "maintainer", 0, at(0))
		trusted := view("trusted", false, "", 5, at(1))
		plain := view("plain", false, "", 0, at(2))

		views := []EndorsementView{plain, trusted, maintainer}
		sortEndorsementViews(views)

		assert.Equal(t, []string{"maintainer", "trusted", "plain"},
			[]string{views[0].Endorsement.ID, views[1].Endorsement.ID, views[2].Endorsement.ID})
	})

	t.Run("empty and singleton inputs don't panic", func(t *testing.T) {
		var empty []EndorsementView
		sortEndorsementViews(empty)
		assert.Empty(t, empty)

		single := []EndorsementView{view("only", false, "", 0, at(0))}
		sortEndorsementViews(single)
		assert.Len(t, single, 1)
	})
}
