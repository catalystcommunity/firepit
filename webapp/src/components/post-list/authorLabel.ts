// Best-effort author display (task C2). `Post`/`Comment` only carry
// `authorId` (a `UserID`) — there is no UserService in csil/firepit.csil v1
// to resolve an id to a handle/display name, a gap the mock's own fixtures
// call out (see `~/lib/mock/fixtures.ts`'s "users" section comment) and the
// real API shares today. The one identity every client can always resolve
// is the viewer's own (whoami's `UserProfile`); for anyone else, the most
// honest thing to show — rather than inventing a fake lookup — is a short,
// stable fragment derived from the id, clearly not styled as a real handle.
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

export function authorLabel(authorId: UserID, viewer: UserProfile | null): string {
  if (viewer && authorId === viewer.id) return viewer.handle;
  return `user-${shortDigest(authorId)}`;
}
