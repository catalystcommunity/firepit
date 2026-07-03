// This file (social.go): friend groups are PRIVATE to their owner —
// PLANDOC.md §4 ("friend_groups ... private to the owner") and §7 task B9's
// explicit acceptance criterion ("groups are private to their owner"). Every
// method below that touches a specific group takes an ownerID and scopes
// its query to "id = ? AND owner_id = ?" (or goes through
// GetOwnedFriendGroup, which does the same); a group ID that exists but
// belongs to someone else is indistinguishable from a group ID that doesn't
// exist at all — both resolve to gorm.ErrRecordNotFound here, and to a
// NotFound AppError (never Forbidden) in SocialService
// (api/internal/csilservices/social.go), per the "never leak existence"
// contract in csilservices/errors.go's NotFound doc comment. There is no
// method here that returns another user's group by ID alone, and there
// must never be one added — that would reopen exactly the leak this
// ownership scoping exists to close. list-friend-groups only ever lists the
// caller's own groups, never anyone else's.

package store

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// FriendGroup mirrors the `friend_groups` table — private to its owner.
type FriendGroup struct {
	ID        string    `gorm:"column:id;type:uuid;primaryKey;default:generate_ulid()"`
	OwnerID   string    `gorm:"column:owner_id;type:uuid;not null"`
	Name      string    `gorm:"column:name;not null"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
}

func (FriendGroup) TableName() string { return "friend_groups" }

// FriendGroupMember mirrors the `friend_group_members` table.
type FriendGroupMember struct {
	GroupID      string    `gorm:"column:group_id;type:uuid;primaryKey"`
	MemberUserID string    `gorm:"column:member_user_id;type:uuid;primaryKey"`
	CreatedAt    time.Time `gorm:"column:created_at;not null"`
}

func (FriendGroupMember) TableName() string { return "friend_group_members" }

// ErrFriendGroupNameTaken is returned by CreateFriendGroup when ownerID
// already has a friend group with the given exact name. There's no DB-level
// unique constraint enforcing this (coredb/migrations is out of scope for
// this task), so CreateFriendGroup closes the race itself: see its doc
// comment for how.
var ErrFriendGroupNameTaken = errors.New("store: friend group name already used by this owner")

// ErrSelfFriend is returned by AddFriendGroupMember when ownerID ==
// memberUserID: adding yourself to your own friend group is meaningless.
// Defense-in-depth alongside SocialService's earlier, field-labeled
// Validation error — see ErrSelfMentionGrant's doc comment in settings.go
// for why both layers check.
var ErrSelfFriend = errors.New("store: cannot add yourself to a friend group")

// FriendGroupWithMembers pairs a FriendGroup with its member user IDs, in
// the shape ListFriendGroupsByOwner returns them.
type FriendGroupWithMembers struct {
	FriendGroup
	MemberUserIDs []string
}

// ListFriendGroupsByOwner returns every friend group ownerID owns, oldest
// first, each with its member user IDs. Never takes any ID but the caller's
// own — see this file's package-level doc comment.
func (s *Store) ListFriendGroupsByOwner(ctx context.Context, ownerID string) ([]FriendGroupWithMembers, error) {
	var groups []FriendGroup
	if err := s.DB.WithContext(ctx).
		Where("owner_id = ?", ownerID).
		Order("created_at ASC").
		Find(&groups).Error; err != nil {
		return nil, err
	}
	if len(groups) == 0 {
		return []FriendGroupWithMembers{}, nil
	}

	groupIDs := make([]string, len(groups))
	for i, g := range groups {
		groupIDs[i] = g.ID
	}
	var members []FriendGroupMember
	if err := s.DB.WithContext(ctx).
		Where("group_id IN ?", groupIDs).
		Order("created_at ASC").
		Find(&members).Error; err != nil {
		return nil, err
	}
	byGroup := make(map[string][]string, len(groups))
	for _, m := range members {
		byGroup[m.GroupID] = append(byGroup[m.GroupID], m.MemberUserID)
	}

	out := make([]FriendGroupWithMembers, len(groups))
	for i, g := range groups {
		out[i] = FriendGroupWithMembers{FriendGroup: g, MemberUserIDs: byGroup[g.ID]}
	}
	return out, nil
}

// GetOwnedFriendGroup returns groupID if it exists AND is owned by ownerID,
// or gorm.ErrRecordNotFound otherwise — deliberately the same error for
// "doesn't exist" and "exists but isn't yours" (this file's package-level
// doc comment).
func (s *Store) GetOwnedFriendGroup(ctx context.Context, ownerID, groupID string) (*FriendGroup, error) {
	var group FriendGroup
	if err := s.DB.WithContext(ctx).First(&group, "id = ? AND owner_id = ?", groupID, ownerID).Error; err != nil {
		return nil, err
	}
	return &group, nil
}

// CreateFriendGroup creates a new friend group for ownerID, rejecting a
// duplicate name (ErrFriendGroupNameTaken) for that same owner. Since
// coredb has no unique index over (owner_id, name) to lean on, this method
// closes the race itself by taking a row lock on the owner's own users row
// for the duration of the check-then-insert: two concurrent
// CreateFriendGroup calls for the same owner serialize on that lock, so
// only one of two identically-named creates can win.
func (s *Store) CreateFriendGroup(ctx context.Context, ownerID, name string) (*FriendGroup, error) {
	var group FriendGroup
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var owner User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&owner, "id = ?", ownerID).Error; err != nil {
			return err
		}

		var existing int64
		if err := tx.Model(&FriendGroup{}).
			Where("owner_id = ? AND name = ?", ownerID, name).
			Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			return ErrFriendGroupNameTaken
		}

		group = FriendGroup{OwnerID: ownerID, Name: name}
		return tx.Create(&group).Error
	})
	if err != nil {
		return nil, err
	}
	return &group, nil
}

// DeleteFriendGroup deletes groupID if it's owned by ownerID, cascading to
// friend_group_members via the FK's ON DELETE CASCADE (coredb/migrations'
// baseline). Returns gorm.ErrRecordNotFound if groupID doesn't exist or
// isn't ownerID's.
func (s *Store) DeleteFriendGroup(ctx context.Context, ownerID, groupID string) error {
	res := s.DB.WithContext(ctx).Where("id = ? AND owner_id = ?", groupID, ownerID).Delete(&FriendGroup{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// AddFriendGroupMember idempotently adds memberUserID to groupID, which
// must be owned by ownerID (gorm.ErrRecordNotFound otherwise). Returns
// ErrSelfFriend if ownerID == memberUserID.
func (s *Store) AddFriendGroupMember(ctx context.Context, ownerID, groupID, memberUserID string) error {
	if _, err := s.GetOwnedFriendGroup(ctx, ownerID, groupID); err != nil {
		return err
	}
	if ownerID == memberUserID {
		return ErrSelfFriend
	}
	member := FriendGroupMember{GroupID: groupID, MemberUserID: memberUserID}
	return s.DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&member).Error
}

// RemoveFriendGroupMember idempotently removes memberUserID from groupID,
// which must be owned by ownerID (gorm.ErrRecordNotFound otherwise);
// removing a member who isn't (or is no longer) in the group is a no-op.
func (s *Store) RemoveFriendGroupMember(ctx context.Context, ownerID, groupID, memberUserID string) error {
	if _, err := s.GetOwnedFriendGroup(ctx, ownerID, groupID); err != nil {
		return err
	}
	return s.DB.WithContext(ctx).
		Where("group_id = ? AND member_user_id = ?", groupID, memberUserID).
		Delete(&FriendGroupMember{}).Error
}
