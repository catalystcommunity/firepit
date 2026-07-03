package csilservices

import (
	"context"

	"github.com/catalystcommunity/firepit/api/internal/store"
)

// RoleAdmin is the users.roles entry that grants instance-wide admin
// privileges (PLANDOC.md §9 decision 2: board creation is admin-only in
// v1). There's no roles enum at the DB layer (users.roles is a plain
// text[] — coredb/migrations/000001_baseline.sql), so this is just a
// string constant rather than a generated type; every caller should go
// through IsInstanceAdmin rather than comparing against this directly, so
// the representation of "admin-ness" can change in one place later.
const RoleAdmin = "admin"

// IsInstanceAdmin reports whether user carries the instance-wide "admin"
// role. A nil user (an anonymous caller — see reqctx.User) is never an
// admin.
func IsInstanceAdmin(user *store.User) bool {
	if user == nil {
		return false
	}
	for _, r := range user.Roles {
		if r == RoleAdmin {
			return true
		}
	}
	return false
}

// BoardRole looks up userID's board_members role on boardID, returning
// store.BoardRoleMaintainer, store.BoardRoleModerator, or "" if the user
// holds no role on that board at all — a normal state (most users aren't
// maintainers or moderators of a given board), not an error. An empty
// userID or boardID always resolves to ("", nil) rather than issuing a
// query, so callers don't need to special-case an anonymous caller (who
// has no user id to look up) before calling this.
//
// This is the shared authz primitive every service that cares about
// board-level roles (ThreadService moderation, EndorsementService role
// badges, etc.) should build on, rather than querying board_members
// directly — see BoardService's authz matrix in board.go for the
// canonical example of composing this with IsInstanceAdmin.
func BoardRole(ctx context.Context, st *store.Store, userID, boardID string) (string, error) {
	if userID == "" || boardID == "" {
		return "", nil
	}
	role, err := st.GetBoardMemberRole(ctx, boardID, userID)
	if err != nil {
		if store.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	return role, nil
}

// IsBoardMaintainer is a convenience wrapper around BoardRole for the
// common single-role check.
func IsBoardMaintainer(ctx context.Context, st *store.Store, userID, boardID string) (bool, error) {
	role, err := BoardRole(ctx, st, userID, boardID)
	return role == store.BoardRoleMaintainer, err
}

// IsBoardModerator is a convenience wrapper around BoardRole for the
// common single-role check.
func IsBoardModerator(ctx context.Context, st *store.Store, userID, boardID string) (bool, error) {
	role, err := BoardRole(ctx, st, userID, boardID)
	return role == store.BoardRoleModerator, err
}
