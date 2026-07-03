package csilservices

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/store"
)

// list-posts pagination: cursor + defaults.
//
// The wire contract (csil/types/threads.csil) only says list-posts is
// "cursor-paginated ... newest activity first" and that PageCursor is an
// opaque string — clients must treat it as a black box and simply echo
// back whatever next_cursor they were handed. That opacity is exactly what
// lets the encoding below change later without a wire-compat break.
const (
	// defaultPostPageLimit is used when ListPostsRequest.Limit is absent.
	defaultPostPageLimit = 50
	// maxPostPageLimit mirrors the wire schema's `.le 200` cap on
	// ListPostsRequest.limit; list-posts has no declared ServiceError arm
	// (csil/firepit.csil), so an out-of-range limit is clamped rather than
	// rejected — see csilservices' package doc comment on why an
	// infallible op should reserve returning an error for genuine faults.
	maxPostPageLimit = 200
)

// postCursor is the decoded form of a PageCursor for list-posts: the
// (last_activity_at, id) of the last row the caller has already seen,
// matching the keyset predicate store.ListPostsByBoard applies (see its
// doc comment for why this row-value comparison, rather than OFFSET, is
// what makes pagination stable under concurrent inserts).
type postCursor struct {
	ActivityAt time.Time
	ID         string
}

// cursorSeparator joins the cursor's two fields before base64url-encoding.
// Safe because a post id is a uuid (hex digits and '-' only — see
// store.NewCommentID's doc comment on ltree labels for the same alphabet
// fact) and therefore never contains this character.
const cursorSeparator = "."

// encodePostCursor renders c as an opaque PageCursor: base64url(no
// padding) of "<activity_at as UnixNano>.<id>". Callers must not construct
// or parse this by hand — decodePostCursor is the only supported reader,
// and its format may change across releases since PageCursor is documented
// as opaque on the wire.
func encodePostCursor(c postCursor) csil.PageCursor {
	raw := strconv.FormatInt(c.ActivityAt.UnixNano(), 10) + cursorSeparator + c.ID
	return csil.PageCursor(base64.RawURLEncoding.EncodeToString([]byte(raw)))
}

// decodePostCursor reverses encodePostCursor. A malformed cursor (the
// client mangled it, or it was minted by some future/incompatible version)
// returns an error; ListPosts treats that as "no cursor" (start from the
// first page) rather than failing the whole request — see ListPosts's doc
// comment for why silently recovering, not erroring, is the right call for
// an op with no declared error arm.
func decodePostCursor(pc csil.PageCursor) (postCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(string(pc))
	if err != nil {
		return postCursor{}, fmt.Errorf("csilservices: malformed page cursor: %w", err)
	}
	nanosStr, id, ok := strings.Cut(string(decoded), cursorSeparator)
	if !ok || id == "" {
		return postCursor{}, fmt.Errorf("csilservices: malformed page cursor: missing separator")
	}
	nanos, err := strconv.ParseInt(nanosStr, 10, 64)
	if err != nil {
		return postCursor{}, fmt.Errorf("csilservices: malformed page cursor: bad timestamp: %w", err)
	}
	return postCursor{ActivityAt: time.Unix(0, nanos).UTC(), ID: id}, nil
}

// postPageLimit clamps an optional wire-supplied limit to
// [1, maxPostPageLimit], defaulting to defaultPostPageLimit when absent.
func postPageLimit(limit *uint64) int {
	if limit == nil {
		return defaultPostPageLimit
	}
	l := *limit
	if l == 0 {
		return defaultPostPageLimit
	}
	if l > maxPostPageLimit {
		return maxPostPageLimit
	}
	return int(l)
}

// toPostListCursor adapts a decoded postCursor to the store package's
// pagination type (kept store-agnostic so the cursor's wire encoding
// doesn't leak into the store layer).
func (c postCursor) toStoreCursor() *store.PostListCursor {
	return &store.PostListCursor{LastActivityAt: c.ActivityAt, ID: c.ID}
}
