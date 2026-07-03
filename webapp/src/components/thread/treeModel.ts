// Builds the parent -> children groupings a nested tree render needs from
// `Thread.comments` (task C3, PLANDOC.md §4: "one indexed query, ordered
// depth-first — renders both reddit-style trees and flat mailing-list
// order"). The server delivers one flat, depth-first-ordered array; this
// module never re-sorts it — grouping by `parentCommentId` while preserving
// each comment's position relative to its siblings is enough to recover the
// nested shape, and the flat "mailing-list" view (CommentTree.tsx) renders
// the very same array untouched.
import type { Comment } from "~/gen/types.gen";

export interface CommentTreeModel {
  /** Top-level comments (direct replies to the post), in server order. */
  roots: Comment[];
  /** A comment's direct replies, in server order. */
  childrenOf(commentId: string): Comment[];
  /** Nesting depth of a comment; top-level replies to the post are depth 0. */
  depthOf(commentId: string): number;
  /** Every descendant (children, grandchildren, ...) of a comment, any order. */
  descendantIds(commentId: string): string[];
}

export function buildCommentTree(comments: readonly Comment[]): CommentTreeModel {
  const byParent = new Map<string | undefined, Comment[]>();
  for (const c of comments) {
    const key = c.parentCommentId;
    const bucket = byParent.get(key);
    if (bucket) bucket.push(c);
    else byParent.set(key, [c]);
  }

  const depth = new Map<string, number>();
  const computeDepth = (c: Comment): number => {
    const cached = depth.get(c.id);
    if (cached !== undefined) return cached;
    if (!c.parentCommentId) {
      depth.set(c.id, 0);
      return 0;
    }
    const parent = comments.find((p) => p.id === c.parentCommentId);
    const d = parent ? computeDepth(parent) + 1 : 0;
    depth.set(c.id, d);
    return d;
  };
  for (const c of comments) computeDepth(c);

  const descendantsCache = new Map<string, string[]>();
  const computeDescendants = (commentId: string): string[] => {
    const cached = descendantsCache.get(commentId);
    if (cached) return cached;
    const direct = byParent.get(commentId) ?? [];
    const all: string[] = [];
    for (const child of direct) {
      all.push(child.id, ...computeDescendants(child.id));
    }
    descendantsCache.set(commentId, all);
    return all;
  };

  return {
    roots: byParent.get(undefined) ?? [],
    childrenOf: (commentId: string) => byParent.get(commentId) ?? [],
    depthOf: (commentId: string) => depth.get(commentId) ?? 0,
    descendantIds: (commentId: string) => computeDescendants(commentId),
  };
}
