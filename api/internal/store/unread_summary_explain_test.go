//go:build integration

package store

// This file is `package store` (not store_test) deliberately: it needs
// unreadSummaryQuery, the unexported SQL text behind UnreadSummary, to run
// EXPLAIN against it directly rather than only through the Go method — see
// TestUnreadSummaryQueryUsesIndexes below. Everything else in this package's
// integration coverage (subscription_test.go, read_test.go) lives in the
// external store_test package and doesn't need this.

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"gorm.io/gorm"

	"github.com/catalystcommunity/firepit/coredb"
)

// TestUnreadSummaryQueryUsesIndexes seeds a modest-but-nontrivial dataset
// (enough boards/posts/comments that the planner has real choices to make),
// then EXPLAIN (ANALYZE)s unreadSummaryQuery and asserts the plan actually
// leans on the indexes the query was designed around:
//
//   - subscriptions_user_id_target_type_target_id_key (the UNIQUE(user_id,
//     target_type, target_id) constraint's index) for "my_subs" — user_id is
//     its leading column.
//   - posts_board_id_idx for the board -> posts fan-out in scoped_posts.
//   - comments_post_id_created_at_idx for unread_comments' per-post,
//     created_at-bounded scan — this is the index the whole query is built
//     to hit, since comments is the table expected to dominate row count in
//     a real deployment.
//
// `SET LOCAL enable_seqscan = off` is used to make the assertion robust
// against Postgres's small-table sequential-scan bias (test data here is a
// few hundred rows; a real deployment's comments table is not) — this
// doesn't fake index *applicability*, it just removes the cost bias that
// would otherwise hide it: if no usable index existed for a predicate,
// disabling seqscan wouldn't conjure one, the planner would still have to
// fall back to a sequential scan. Seeing the named indexes in the plan here
// is proof they're wired to match the query's actual predicates, not proof
// of what a production planner picks (that's cost-based and does its own
// thing, correctly, at scale).
func TestUnreadSummaryQueryUsesIndexes(t *testing.T) {
	ctx := context.Background()

	pgContainer, err := tcpostgres.Run(ctx,
		"postgres:17",
		tcpostgres.WithDatabase("firepit_test"),
		tcpostgres.WithUsername("firepit"),
		tcpostgres.WithPassword("devpass123"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second)),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pgContainer.Terminate(ctx)) })

	dsn, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	require.NoError(t, coredb.Up(dsn))

	gdb, err := Open(dsn)
	require.NoError(t, err)
	st := New(gdb)

	viewer := seedExplainUser(t, st, "viewer")
	author := seedExplainUser(t, st, "author")

	const boardCount = 5
	const postsPerBoard = 10
	const commentsPerPost = 8

	var boardIDs []string
	for b := 0; b < boardCount; b++ {
		board := &Board{Slug: seedSlug("board", b), Title: "Board", Kind: "discussion", CreatedBy: author.ID}
		require.NoError(t, st.DB.WithContext(ctx).Create(board).Error)
		boardIDs = append(boardIDs, board.ID)

		// Subscribe the viewer to every board so scoped_posts and the
		// downstream comment/post scans have real work across all of them.
		_, err := st.Subscribe(ctx, viewer.ID, "board", board.ID)
		require.NoError(t, err)

		for p := 0; p < postsPerBoard; p++ {
			post := &Post{BoardID: board.ID, AuthorID: author.ID, Title: "post", LastActivityAt: time.Now().UTC()}
			require.NoError(t, st.DB.WithContext(ctx).Create(post).Error)

			for c := 0; c < commentsPerPost; c++ {
				comment := &Comment{
					PostID:   post.ID,
					AuthorID: author.ID,
					Path:     Ltree(post.ID),
					BodyMD:   "reply",
					Origin:   "user",
				}
				require.NoError(t, st.DB.WithContext(ctx).Create(comment).Error)
			}
		}
	}

	// Fresh statistics so the planner's row-count estimates reflect the
	// seeded data rather than defaults.
	require.NoError(t, st.DB.Exec("ANALYZE subscriptions, posts, comments, read_marks, unread_overrides").Error)

	var planLines []string
	err = st.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Disabling seqscan alone isn't enough to see the per-post,
		// created_at-bounded comments index: with this little data, a hash
		// join that reads the whole (still index-scanned, since seqscan is
		// off) comments table once is cheaper than a nested loop probing
		// comments_post_id_created_at_idx once per scoped post. Also
		// disabling hash/merge join forces the nested-loop shape a real
		// deployment's planner would pick anyway once comments vastly
		// outnumbers any one user's scoped posts — that's the shape this
		// index exists for.
		for _, stmt := range []string{
			"SET LOCAL enable_seqscan = off",
			"SET LOCAL enable_hashjoin = off",
			"SET LOCAL enable_mergejoin = off",
		} {
			if err := tx.Exec(stmt).Error; err != nil {
				return err
			}
		}
		rows, err := tx.Raw("EXPLAIN (ANALYZE, FORMAT TEXT) "+unreadSummaryQuery, sql.Named("user_id", viewer.ID)).Rows()
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				return err
			}
			planLines = append(planLines, line)
		}
		return rows.Err()
	})
	require.NoError(t, err)

	planText := strings.Join(planLines, "\n")
	t.Logf("unread-summary query plan:\n%s", planText)

	require.Contains(t, planText, "subscriptions_user_id_target_type_target_id_key",
		"expected my_subs to use the UNIQUE(user_id, target_type, target_id) index")
	require.True(t,
		strings.Contains(planText, "posts_board_id_idx") || strings.Contains(planText, "posts_board_id_last_activity_at_idx"),
		"expected the board -> posts fan-out in scoped_posts to use a board_id-keyed index")
	require.Contains(t, planText, "comments_post_id_created_at_idx",
		"expected unread_comments to use comments_post_id_created_at_idx")
}

func seedExplainUser(t *testing.T, st *Store, handle string) *User {
	t.Helper()
	u := &User{LinkkeysDomain: "example.com", LinkkeysUserID: handle, Handle: handle, Kind: "human"}
	require.NoError(t, st.DB.WithContext(context.Background()).Create(u).Error)
	return u
}

func seedSlug(prefix string, n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	return prefix + "-" + string(letters[n%len(letters)]) + string(rune('0'+n))
}
