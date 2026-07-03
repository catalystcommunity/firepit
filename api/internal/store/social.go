package store

import "time"

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
