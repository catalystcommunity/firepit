// One comment in tree mode: the card, a collapse toggle on the rail, and
// (recursively) its replies — task C3 scope item 1: "thread depth shown by
// thin indent rails ... collapse affordances on the rail". Indentation is
// just nested `<ul class="comment-children">`s in index.css; no depth math
// needed here beyond what the recursion itself already expresses.
import { createMemo, For, Show, type Component } from "solid-js";
import type { Comment, UserProfile } from "~/gen/types.gen";
import type { CommentTreeModel } from "./treeModel";
import type { ThreadActions } from "./threadActions";
import ContentCard from "./ContentCard";

export interface CommentNodeProps {
  comment: Comment;
  model: CommentTreeModel;
  viewer: UserProfile | null;
  ctx: ThreadActions;
  collapsed: Set<string>;
  onToggleCollapse: (commentId: string) => void;
}

const CommentNode: Component<CommentNodeProps> = (props) => {
  const children = createMemo(() => props.model.childrenOf(props.comment.id));
  const isCollapsed = () => props.collapsed.has(props.comment.id);
  const hiddenCount = createMemo(() => props.model.descendantIds(props.comment.id).length);

  return (
    <li class="comment-node">
      <div class="comment-rail">
        <Show when={children().length > 0}>
          <button
            type="button"
            class="collapse-toggle"
            onClick={() => props.onToggleCollapse(props.comment.id)}
            aria-label={isCollapsed() ? "Expand replies" : "Collapse replies"}
            aria-expanded={!isCollapsed()}
          >
            {isCollapsed() ? "+" : "–"}
          </button>
        </Show>
      </div>
      <div class="comment-body-col">
        <ContentCard
          targetType="comment"
          id={props.comment.id}
          bodyMd={props.comment.bodyMd}
          authorId={props.comment.authorId}
          authorHandle={props.comment.authorHandle}
          origin={props.comment.origin}
          originRef={props.comment.originRef}
          createdAt={props.comment.createdAt}
          editedAt={props.comment.editedAt}
          deletedAt={props.comment.deletedAt}
          viewer={props.viewer}
          ctx={props.ctx}
          showReplyToggle
          anchorId={`c-${props.comment.id}`}
        />

        <Show when={isCollapsed()} fallback={
          <Show when={children().length > 0}>
            <ul class="comment-children">
              <For each={children()}>
                {(child) => (
                  <CommentNode
                    comment={child}
                    model={props.model}
                    viewer={props.viewer}
                    ctx={props.ctx}
                    collapsed={props.collapsed}
                    onToggleCollapse={props.onToggleCollapse}
                  />
                )}
              </For>
            </ul>
          </Show>
        }>
          <p class="collapsed-summary">
            {hiddenCount()} {hiddenCount() === 1 ? "reply" : "replies"} hidden
          </p>
        </Show>
      </div>
    </li>
  );
};

export default CommentNode;
