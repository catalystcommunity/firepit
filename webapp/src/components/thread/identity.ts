// Author/endorser identity display (task C3). `Post.authorId`,
// `Comment.authorId`, and `Endorsement.userId` now come with a denormalized
// `author_handle` (a CSIL schema follow-up: ThreadService/EndorsementService
// populate it server-side via a single batched lookup — see
// api/internal/csilservices/thread.go's ListPosts/GetThread and
// endorsement.go's ListEndorsements) — this module prefers that real handle
// whenever the caller has it.
//
// A handle can still legitimately be absent (a deleted/tombstoned author is
// blanked, or — defensively — a users row that's somehow gone), so
// `shortUserRef`'s hashed stand-in remains as the fallback rather than
// rendering a raw/blank id.
import type { OriginKind, UserProfile } from "~/gen/types.gen";

export interface AuthorLabel {
  /** What to render as the author's name. */
  label: string;
  /** True when this is the signed-in viewer's own content. */
  isSelf: boolean;
}

/**
 * A short, stable, readable stand-in for a user id we can't resolve to a
 * name (no `author_handle` on the response — see this module's doc comment
 * for when that happens). Hashed (FNV-1a) rather than a raw substring: this
 * repo's own mock fixture ids (see fixtures.ts) are hand-written,
 * human-readable fake ULIDs that all happen to share the same zero-padded
 * tail (`...0000000`), so slicing the last few characters made every
 * distinct user render as the identical "user-000000" — silently
 * unreadable. Hashing the whole id spreads different ids across visibly
 * different labels instead.
 */
export function shortUserRef(userId: string): string {
  let hash = 0x811c9dc5;
  for (let i = 0; i < userId.length; i++) {
    hash ^= userId.charCodeAt(i);
    hash = Math.imul(hash, 0x01000193);
  }
  const code = (hash >>> 0).toString(36).padStart(6, "0").slice(0, 6);
  return `user-${code}`;
}

export function describeAuthor(
  authorId: string,
  authorHandle: string | undefined,
  origin: OriginKind,
  viewer: UserProfile | null | undefined,
): AuthorLabel {
  if (viewer && authorId === viewer.id) {
    return { label: viewer.displayName, isSelf: true };
  }
  if (origin === "github") {
    // The origin glyph in the UI already communicates "this is GitHub
    // content" — no need to resolve the per-mapping system user's name.
    return { label: "GitHub", isSelf: false };
  }
  if (origin === "system") {
    return { label: "Firepit", isSelf: false };
  }
  if (authorHandle) {
    return { label: `@${authorHandle}`, isSelf: false };
  }
  return { label: shortUserRef(authorId), isSelf: false };
}

/**
 * Best-effort backlink URL out of a post/comment's `origin_ref` (opaque
 * JSON text — see `Post.originRef`'s doc comment). GitHub webhook payloads
 * commonly carry the source under one of these keys; anything else (or
 * unparseable JSON) just means no backlink renders.
 */
export function parseOriginBacklink(originRef: string | undefined): string | undefined {
  if (!originRef) return undefined;
  try {
    const parsed: unknown = JSON.parse(originRef);
    if (!parsed || typeof parsed !== "object") return undefined;
    const rec = parsed as Record<string, unknown>;
    const candidate = rec.url ?? rec.htmlUrl ?? rec.html_url ?? rec.link;
    return typeof candidate === "string" ? candidate : undefined;
  } catch {
    return undefined;
  }
}
