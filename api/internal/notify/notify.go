// Package notify defines the internal event contract between content-writing
// services (threads, endorsements) and the notification fan-out.
//
// Writers call Publisher.Publish inside the same database transaction as the
// content write, so notification rows commit atomically with the content that
// caused them (PLANDOC §4). The tx parameter is that open transaction.
//
// The fan-out implementation lives in this package (task B7). Until it lands,
// wire Noop. Service constructors accept the Publisher interface, never a
// concrete type.
package notify

import (
	"context"

	"gorm.io/gorm"
)

type Kind string

const (
	// KindContentCreated covers both new posts (CommentID empty) and new
	// comments (CommentID set).
	KindContentCreated Kind = "content_created"
	// KindContentEdited is published on edits so mention fan-out can run for
	// newly added @handles. Notification of subscribers does NOT re-fire.
	KindContentEdited Kind = "content_edited"
	// KindEndorsed notifies the content author, gated by notify_on_endorse.
	KindEndorsed Kind = "endorsed"
)

// Event carries everything fan-out needs without refetching inside the
// transaction. Body is the raw markdown (for @handle parsing); empty for
// KindEndorsed.
type Event struct {
	Kind    Kind
	ActorID string
	BoardID string
	PostID  string
	// CommentID is empty for post-level events.
	CommentID string
	// TargetType/TargetID identify the endorsed item for KindEndorsed
	// ("post"|"comment"); empty otherwise.
	TargetType string
	TargetID   string
	Body       string
}

type Publisher interface {
	Publish(ctx context.Context, tx *gorm.DB, e Event) error
}

// Noop discards events; the wiring default until the fan-out lands.
type Noop struct{}

func (Noop) Publish(context.Context, *gorm.DB, Event) error { return nil }
