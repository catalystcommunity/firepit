// Renders a post's comments either as a nested, collapsible tree or as a
// flat "mailing-list order" list (task C3 scope item 1). Both modes render
// the exact same `comments` array from `Thread` — tree mode groups it by
// `parentCommentId` (see treeModel.ts), flat mode renders it untouched, in
// server order, depth-first — never re-sorted by either mode.
import { createMemo, createSignal, For, type Component } from "solid-js";
import type { Comment, UserProfile } from "~/gen/types.gen";
import { buildCommentTree } from "./treeModel";
import type { ThreadActions } from "./threadActions";
import type { ThreadViewMode } from "./viewMode";
import CommentNode from "./CommentNode";
import ContentCard from "./ContentCard";

export interface CommentTreeProps {
  comments: Comment[];
  mode: ThreadViewMode;
  viewer: UserProfile | null;
  ctx: ThreadActions;
}

const CommentTree: Component<CommentTreeProps> = (props) => {
  const model = createMemo(() => buildCommentTree(props.comments));
  const [collapsed, setCollapsed] = createSignal<Set<string>>(new Set());

  const toggleCollapse = (commentId: string): void => {
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(commentId)) next.delete(commentId);
      else next.add(commentId);
      return next;
    });
  };

  return (
    <div class="comment-tree-root">
      {props.mode === "tree" ? (
        <ul class="comment-tree" role="list">
          <For each={model().roots}>
            {(c) => (
              <CommentNode
                comment={c}
                model={model()}
                viewer={props.viewer}
                ctx={props.ctx}
                collapsed={collapsed()}
                onToggleCollapse={toggleCollapse}
              />
            )}
          </For>
        </ul>
      ) : (
        <ol class="comment-flat" role="list">
          <For each={props.comments}>
            {(c) => (
              <li class="flat-row">
                <span class="depth-badge" aria-hidden="true">
                  {"›".repeat(Math.min(model().depthOf(c.id) + 1, 8))}
                </span>
                <ContentCard
                  targetType="comment"
                  id={c.id}
                  bodyMd={c.bodyMd}
                  authorId={c.authorId}
                  origin={c.origin}
                  originRef={c.originRef}
                  createdAt={c.createdAt}
                  editedAt={c.editedAt}
                  deletedAt={c.deletedAt}
                  viewer={props.viewer}
                  ctx={props.ctx}
                  showReplyToggle
                  anchorId={`c-${c.id}`}
                />
              </li>
            )}
          </For>
        </ol>
      )}
    </div>
  );
};

export default CommentTree;
