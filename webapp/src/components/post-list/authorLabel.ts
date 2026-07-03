// Author display for the board post list (task C2). `Post.authorHandle` is
// now denormalized server-side (a CSIL schema follow-up — see
// api/internal/csilservices/thread.go's ListPosts, which resolves it via one
// batched lookup, never per-row), so this prefers that real handle. A
// missing handle (tombstoned author, or — defensively — a `users` row
// that's somehow gone) still falls back to a short, stable, clearly-not-a-
// real-handle fragment derived from the id rather than showing nothing.
import type { UserID, UserProfile } from "~/gen/types.gen";

// A short deterministic digest, not a substring: the mock's own fixture ids
// (see fixtures.ts's comment — "shaped the same but spell out what they
// name") are hand-authored and zero-padded, so a plain `.slice()` off either
// end collides for every fixture user. Real ULIDs wouldn't collide like
// this, but hashing the whole id avoids relying on that.
function shortDigest(id: string): string {
  let hash = 0;
  for (let i = 0; i < id.length; i += 1) {
    hash = (hash * 31 + id.charCodeAt(i)) | 0;
  }
  return Math.abs(hash).toString(36).slice(0, 6).padStart(6, "0");
}

export function authorLabel(authorId: UserID, authorHandle: string | undefined, viewer: UserProfile | null): string {
  if (viewer && authorId === viewer.id) return viewer.handle;
  if (authorHandle) return authorHandle;
  return `user-${shortDigest(authorId)}`;
}
