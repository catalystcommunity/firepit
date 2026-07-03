// Renders one post or comment: header (author/timestamps/edited/origin),
// markdown body, endorsements, edit/delete-own-content, revision history,
// and (for comments) a reply toggle — task C3 scope items 1, 3, 4. Shared
// between PostView and CommentNode so both stay in sync as this evolves.
import { createSignal, Show, type Component } from "solid-js";
import type { OriginKind, TargetType, UserProfile } from "~/gen/types.gen";
import Composer from "./Composer";
import ContentMeta from "./ContentMeta";
import Endorsements from "./Endorsements";
import MarkdownBody from "./MarkdownBody";
import RevisionHistory from "./RevisionHistory";
import type { ThreadActions } from "./threadActions";

export interface ContentCardProps {
  targetType: TargetType;
  id: string;
  title?: string;
  bodyMd: string;
  authorId: string;
  origin: OriginKind;
  originRef?: string;
  createdAt: Date;
  editedAt?: Date;
  deletedAt?: Date;
  viewer: UserProfile | null;
  ctx: ThreadActions;
  /** Comments show a reply toggle; the root post's reply box lives in ThreadPage instead. */
  showReplyToggle: boolean;
  /** Anchor target for permalinks (`#c-<id>`/`#post`). */
  anchorId: string;
}

const ContentCard: Component<ContentCardProps> = (props) => {
  const [editing, setEditing] = createSignal(false);
  const [replying, setReplying] = createSignal(false);

  const isOwn = () => !!props.viewer && props.viewer.id === props.authorId;
  const isDeleted = () => !!props.deletedAt;
  const unread = () => props.ctx.isUnread(props.targetType as "post" | "comment", props.id, props.createdAt);
  const pinnedUnread = () => props.ctx.isUnreadOverride(props.targetType as "post" | "comment", props.id);

  const submitEdit = async (values: { title?: string; bodyMd: string }) => {
    if (props.targetType === "post") {
      await props.ctx.editPost(values.title ?? props.title ?? "", values.bodyMd);
    } else {
      await props.ctx.editComment(props.id, values.bodyMd);
    }
    setEditing(false);
  };

  const submitReply = async (values: { bodyMd: string }) => {
    await props.ctx.reply(props.targetType === "comment" ? props.id : undefined, values.bodyMd);
    setReplying(false);
  };

  const remove = async () => {
    if (typeof window !== "undefined" && !window.confirm("Delete this? It will remain as a placeholder in the thread."))
      return;
    if (props.targetType === "post") await props.ctx.deletePost();
    else await props.ctx.deleteComment(props.id);
  };

  return (
    <article
      id={props.anchorId}
      class="content-card"
      classList={{
        "is-post": props.targetType === "post",
        "is-comment": props.targetType === "comment",
        "is-deleted": isDeleted(),
        "is-unread": unread() && !isDeleted(),
      }}
    >
      <Show when={props.title && !isDeleted()}>
        <h1 class="post-title">{props.title}</h1>
      </Show>

      <ContentMeta
        authorId={props.authorId}
        origin={props.origin}
        originRef={props.originRef}
        createdAt={props.createdAt}
        editedAt={props.editedAt}
        viewer={props.viewer}
      />

      <Show
        when={!isDeleted()}
        fallback={<p class="tombstone">[deleted]</p>}
      >
        <Show
          when={!editing()}
          fallback={
            <Composer
              initialBody={props.bodyMd}
              initialTitle={props.title}
              showTitle={props.targetType === "post"}
              submitLabel="Save"
              mentionCandidates={props.ctx.mentionCandidates}
              onSubmit={submitEdit}
              onCancel={() => setEditing(false)}
            />
          }
        >
          <MarkdownBody source={props.bodyMd} />
        </Show>
      </Show>

      <Show when={!isDeleted()}>
        <Endorsements
          targetType={props.targetType}
          targetId={props.id}
          authorId={props.authorId}
          isDeleted={isDeleted()}
          viewer={props.viewer}
        />
      </Show>

      <div class="content-actions">
        <a class="link-button permalink" href={`#${props.anchorId}`}>
          permalink
        </a>

        <Show when={props.showReplyToggle && props.viewer && !editing()}>
          <button type="button" class="link-button" onClick={() => setReplying((v) => !v)}>
            {replying() ? "Cancel reply" : "Reply"}
          </button>
        </Show>

        <Show when={isOwn() && !isDeleted() && !editing()}>
          <button type="button" class="link-button" onClick={() => setEditing(true)}>
            Edit
          </button>
          <button type="button" class="link-button danger" onClick={() => void remove()}>
            Delete
          </button>
        </Show>

        <Show when={props.viewer && !isDeleted()}>
          <button
            type="button"
            class="link-button"
            onClick={() =>
              void props.ctx.toggleReadOverride(props.targetType as "post" | "comment", props.id, props.createdAt)
            }
          >
            {pinnedUnread() || unread() ? "Mark read" : "Mark unread"}
          </button>
        </Show>

        <Show when={props.editedAt && !isDeleted()}>
          <RevisionHistory targetType={props.targetType} targetId={props.id} />
        </Show>
      </div>

      <Show when={replying()}>
        <Composer
          submitLabel="Reply"
          mentionCandidates={props.ctx.mentionCandidates}
          onSubmit={submitReply}
          onCancel={() => setReplying(false)}
        />
      </Show>
    </article>
  );
};

export default ContentCard;
