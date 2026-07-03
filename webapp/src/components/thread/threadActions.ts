// The bag of callbacks + shared context every content card (post or
// comment) needs, assembled once in ThreadPage.tsx and threaded down
// through CommentTree -> CommentNode -> ContentCard. Kept as one object
// (rather than half a dozen individual props at every layer) since every
// card needs all of it.
import type { UserProfile } from "~/gen/types.gen";

export interface ThreadActions {
  viewer: UserProfile | null;
  /** Handles seen in the thread so far — MentionAutocomplete's candidate pool. */
  mentionCandidates: string[];
  reply(parentCommentId: string | undefined, bodyMd: string): Promise<void>;
  editComment(id: string, bodyMd: string): Promise<void>;
  deleteComment(id: string): Promise<void>;
  editPost(title: string, bodyMd: string): Promise<void>;
  deletePost(): Promise<void>;
  /** Whether an item should render with the subtle "unread" treatment. */
  isUnread(kind: "post" | "comment", id: string, createdAt: Date): boolean;
  /** Whether an item currently carries an explicit "keep unread" pin. */
  isUnreadOverride(kind: "post" | "comment", id: string): boolean;
  toggleReadOverride(kind: "post" | "comment", id: string, createdAt: Date): Promise<void>;
}
