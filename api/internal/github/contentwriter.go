package github

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/catalystcommunity/firepit/api/internal/content"
	"github.com/catalystcommunity/firepit/api/internal/notify"
	"github.com/catalystcommunity/firepit/api/internal/store"
)

// systemUserLinkkeysDomain is the synthetic linkkeys_domain every GitHub
// ingestion system user is created under. Combined with the repo string as
// linkkeys_user_id, it satisfies the users table's
// UNIQUE(linkkeys_domain, linkkeys_user_id) constraint — exactly one system
// user per repo, safe to get-or-create idempotently.
const systemUserLinkkeysDomain = "github.firepit.system"

// contentWriter is the B8-owned path from a verified GitHub webhook event to
// firepit content. It resolves/creates the repo's system user, applies the
// event -> content rules (see the package doc comment's "Event rules v1"
// section), and inserts the resulting post/top-level comment via
// api/internal/content — the same shared creation logic
// ThreadService.CreatePost/CreateComment uses, so a GitHub-originated post or
// comment fans out notify.Publisher events exactly like a human-authored one
// (see content's package doc comment for which validations that shared path
// deliberately skips for system content, and why).
type contentWriter struct {
	store  *store.Store
	notify notify.Publisher
}

// systemUserIdentity derives the (linkkeys_domain, linkkeys_user_id, handle,
// display_name) a repo's system user is created/looked-up with. See the
// package doc comment's "System user per mapping" section.
func systemUserIdentity(repo string) (linkkeysDomain, linkkeysUserID, handle, displayName string) {
	sanitized := strings.NewReplacer("/", "-", "_", "-").Replace(strings.ToLower(repo))
	return systemUserLinkkeysDomain, repo, "github-" + sanitized, "GitHub: " + repo
}

// getOrCreateSystemUser returns the system user for repo, creating it on
// first use. A unique-constraint race (two concurrent first-events for the
// same brand-new mapping) is handled by re-querying on a failed insert
// rather than erroring — whichever request's Create won, both end up using
// the same row.
func (w *contentWriter) getOrCreateSystemUser(ctx context.Context, repo string) (*store.User, error) {
	domain, uid, handle, displayName := systemUserIdentity(repo)

	var existing store.User
	err := w.store.DB.WithContext(ctx).
		Where("linkkeys_domain = ? AND linkkeys_user_id = ?", domain, uid).
		First(&existing).Error
	if err == nil {
		return &existing, nil
	}
	if !store.IsNotFound(err) {
		return nil, err
	}

	toCreate := store.User{
		LinkkeysDomain: domain,
		LinkkeysUserID: uid,
		Handle:         handle,
		DisplayName:    displayName,
		Kind:           "system",
		Roles:          store.StringArray{},
	}
	if err := w.store.DB.WithContext(ctx).Create(&toCreate).Error; err != nil {
		// Lost a create race with another delivery for the same repo;
		// the row now exists, so fetch it instead of failing the event.
		var raced store.User
		if ferr := w.store.DB.WithContext(ctx).
			Where("linkkeys_domain = ? AND linkkeys_user_id = ?", domain, uid).
			First(&raced).Error; ferr == nil {
			return &raced, nil
		}
		return nil, err
	}
	return &toCreate, nil
}

// findPostByOriginRef returns the post previously created for (repo, kind,
// number) — e.g. the post an issue's "opened" event created, looked up
// again when its "closed" event arrives — or (nil, nil) if there is none
// yet (never IsNotFound; callers branch on a nil pointer instead of an
// error, since "not found" is an expected, common case here, not a
// failure).
func (w *contentWriter) findPostByOriginRef(ctx context.Context, repo, kind string, number int) (*store.Post, error) {
	var p store.Post
	err := w.store.DB.WithContext(ctx).
		Where("origin = ? AND origin_ref ->> 'repo' = ? AND origin_ref ->> 'kind' = ? AND origin_ref ->> 'number' = ?",
			"github", repo, kind, strconv.Itoa(number)).
		First(&p).Error
	if err != nil {
		if store.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

// deliveryProcessed reports whether a post or comment already carries
// deliveryID in its origin_ref — i.e. this exact GitHub delivery (a
// redelivery after a timeout, a manual "redeliver" from the GitHub UI,
// etc.) has already been applied. Handler.ServeHTTP treats true as a no-op,
// never re-applying the event.
func (w *contentWriter) deliveryProcessed(ctx context.Context, deliveryID string) (bool, error) {
	if deliveryID == "" {
		// No delivery id to dedupe on (header absent) — process once,
		// can't detect redeliveries; see the package doc comment.
		return false, nil
	}
	var count int64
	if err := w.store.DB.WithContext(ctx).Model(&store.Post{}).
		Where("origin_ref ->> 'delivery_id' = ?", deliveryID).Count(&count).Error; err != nil {
		return false, err
	}
	if count > 0 {
		return true, nil
	}
	if err := w.store.DB.WithContext(ctx).Model(&store.Comment{}).
		Where("origin_ref ->> 'delivery_id' = ?", deliveryID).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// createPost inserts a new origin='github' post via the shared
// api/internal/content path, publishing the notify.KindContentCreated event
// that follows from it in the same transaction as the insert.
func (w *contentWriter) createPost(ctx context.Context, boardID, authorID, title, bodyMD string, ref OriginRef) (*store.Post, error) {
	refBytes, err := ref.Marshal()
	if err != nil {
		return nil, err
	}

	var post *store.Post
	err = w.store.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var txErr error
		post, txErr = content.CreatePost(ctx, tx, w.store, w.notify, content.CreatePostParams{
			BoardID:   boardID,
			AuthorID:  authorID,
			Title:     title,
			BodyMD:    bodyMD,
			Origin:    "github",
			OriginRef: datatypes.JSON(refBytes),
		})
		return txErr
	})
	if err != nil {
		return nil, err
	}
	return post, nil
}

// createTopLevelComment inserts a new origin='github' comment directly
// under postID (no parent) via the shared api/internal/content path, which
// also bumps the post's comment_count/last_activity_at and publishes the
// resulting notify.KindContentCreated event — all in the same transaction
// as the insert. See the package doc comment: only TOP-LEVEL comments are
// ever created here, so ParentCommentID/ParentPath are left zero (no parent
// path to prepend).
func (w *contentWriter) createTopLevelComment(ctx context.Context, boardID, postID, authorID, bodyMD string, ref OriginRef) (*store.Comment, error) {
	refBytes, err := ref.Marshal()
	if err != nil {
		return nil, err
	}

	var comment *store.Comment
	err = w.store.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var txErr error
		comment, txErr = content.CreateComment(ctx, tx, w.store, w.notify, content.CreateCommentParams{
			BoardID:   boardID,
			PostID:    postID,
			AuthorID:  authorID,
			BodyMD:    bodyMD,
			Origin:    "github",
			OriginRef: datatypes.JSON(refBytes),
		})
		return txErr
	})
	if err != nil {
		return nil, err
	}
	return comment, nil
}

// HandleEvent applies event rules v1 (see the package doc comment) for one
// verified, signature-checked webhook delivery. It returns (false, nil) for
// every "recognized but no action taken" case (event type not subscribed by
// this mapping, or not one of the three event types this package
// understands, or already processed) — Handler.ServeHTTP always answers
// HTTP 200 regardless of this return value; the bool only distinguishes
// "created/commented" from "no-op" for logging.
func (w *contentWriter) HandleEvent(ctx context.Context, mapping *store.GithubMapping, eventType, deliveryID string, body []byte) (bool, error) {
	if !containsString([]string(mapping.Events), eventType) {
		return false, nil
	}
	processed, err := w.deliveryProcessed(ctx, deliveryID)
	if err != nil {
		return false, err
	}
	if processed {
		return false, nil
	}

	switch eventType {
	case "release":
		return w.handleRelease(ctx, mapping, deliveryID, body)
	case "issues":
		return w.handleIssues(ctx, mapping, deliveryID, body)
	case "pull_request":
		return w.handlePullRequest(ctx, mapping, deliveryID, body)
	default:
		return false, nil
	}
}

func (w *contentWriter) handleRelease(ctx context.Context, mapping *store.GithubMapping, deliveryID string, body []byte) (bool, error) {
	var ev releaseEventPayload
	if err := json.Unmarshal(body, &ev); err != nil {
		return false, err
	}
	if ev.Action != "published" {
		return false, nil
	}

	sysUser, err := w.getOrCreateSystemUser(ctx, mapping.Repo)
	if err != nil {
		return false, err
	}

	ref := OriginRef{Repo: mapping.Repo, Kind: "release", Number: int(ev.Release.ID), URL: ev.Release.HTMLURL, DeliveryID: deliveryID}
	title := "Release " + firstNonEmpty(ev.Release.Name, ev.Release.TagName)
	if _, err := w.createPost(ctx, mapping.BoardID, sysUser.ID, title, ev.Release.Body, ref); err != nil {
		return false, err
	}
	return true, nil
}

func (w *contentWriter) handleIssues(ctx context.Context, mapping *store.GithubMapping, deliveryID string, body []byte) (bool, error) {
	var ev issuesEventPayload
	if err := json.Unmarshal(body, &ev); err != nil {
		return false, err
	}

	switch ev.Action {
	case "opened":
		sysUser, err := w.getOrCreateSystemUser(ctx, mapping.Repo)
		if err != nil {
			return false, err
		}
		ref := OriginRef{Repo: mapping.Repo, Kind: "issue", Number: ev.Issue.Number, URL: ev.Issue.HTMLURL, DeliveryID: deliveryID}
		title := issueTitle(ev.Issue.Number, ev.Issue.Title, false)
		if _, err := w.createPost(ctx, mapping.BoardID, sysUser.ID, title, ev.Issue.Body, ref); err != nil {
			return false, err
		}
		return true, nil

	case "closed":
		sysUser, err := w.getOrCreateSystemUser(ctx, mapping.Repo)
		if err != nil {
			return false, err
		}
		existing, err := w.findPostByOriginRef(ctx, mapping.Repo, "issue", ev.Issue.Number)
		if err != nil {
			return false, err
		}
		ref := OriginRef{Repo: mapping.Repo, Kind: "issue", Number: ev.Issue.Number, URL: ev.Issue.HTMLURL, DeliveryID: deliveryID}
		if existing == nil {
			// Fallback: no post exists for this issue yet (mapping added
			// after it was opened) — create one rather than losing the
			// event; see the package doc comment.
			title := issueTitle(ev.Issue.Number, ev.Issue.Title, true)
			if _, err := w.createPost(ctx, mapping.BoardID, sysUser.ID, title, ev.Issue.Body, ref); err != nil {
				return false, err
			}
			return true, nil
		}
		if _, err := w.createTopLevelComment(ctx, existing.BoardID, existing.ID, sysUser.ID, "Issue closed.", ref); err != nil {
			return false, err
		}
		return true, nil

	default:
		return false, nil
	}
}

func (w *contentWriter) handlePullRequest(ctx context.Context, mapping *store.GithubMapping, deliveryID string, body []byte) (bool, error) {
	var ev pullRequestEventPayload
	if err := json.Unmarshal(body, &ev); err != nil {
		return false, err
	}
	// v1 only reacts to a merge (see the package doc comment); "closed"
	// without merged=true (a PR closed unmerged) is dropped.
	if ev.Action != "closed" || !ev.PullRequest.Merged {
		return false, nil
	}

	sysUser, err := w.getOrCreateSystemUser(ctx, mapping.Repo)
	if err != nil {
		return false, err
	}

	ref := OriginRef{Repo: mapping.Repo, Kind: "pull_request", Number: ev.PullRequest.Number, URL: ev.PullRequest.HTMLURL, DeliveryID: deliveryID}
	existing, err := w.findPostByOriginRef(ctx, mapping.Repo, "pull_request", ev.PullRequest.Number)
	if err != nil {
		return false, err
	}
	if existing != nil {
		// Comment mode: whether thread_mode is "post_per_pull_request" (a
		// pre-existing post from a future opened-event rule) or any other
		// mode (a post this same merge fallback created earlier — see
		// below), an existing thread always gets a comment, never a
		// second post.
		if _, err := w.createTopLevelComment(ctx, existing.BoardID, existing.ID, sysUser.ID, "Pull request merged.", ref); err != nil {
			return false, err
		}
		return true, nil
	}

	// No existing thread. v1 doesn't track pull_request "opened", so this
	// is the common case for every mapping regardless of thread_mode —
	// the merge event itself creates the thread. Title differs slightly
	// so the two mapping.ThreadMode settings read distinctly.
	title := "PR #" + strconv.Itoa(ev.PullRequest.Number) + " " + ev.PullRequest.Title
	if mapping.ThreadMode != "post_per_pull_request" {
		title += " (merged)"
	}
	if _, err := w.createPost(ctx, mapping.BoardID, sysUser.ID, title, ev.PullRequest.Body, ref); err != nil {
		return false, err
	}
	return true, nil
}

func issueTitle(number int, title string, closed bool) string {
	t := "#" + strconv.Itoa(number) + " " + title
	if closed {
		t += " (closed)"
	}
	return t
}
