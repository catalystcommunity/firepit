// Shared author/timestamp/edited/origin header line for both PostView and
// CommentNode (task C3 scope item 1: "post header (title, author,
// timestamps, edited marker)"; scope item 1's "GitHub-origin content gets a
// distinct quiet treatment (origin glyph + backlink)").
import { Show, type Component } from "solid-js";
import type { OriginKind, UserProfile } from "~/gen/types.gen";
import { describeAuthor, parseOriginBacklink } from "./identity";

export interface ContentMetaProps {
  authorId: string;
  authorHandle?: string;
  origin: OriginKind;
  originRef?: string;
  createdAt: Date;
  editedAt?: Date;
  viewer: UserProfile | null;
}

const stamp = (d: Date): string => d.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });

const ContentMeta: Component<ContentMetaProps> = (props) => {
  const author = () => describeAuthor(props.authorId, props.authorHandle, props.origin, props.viewer);
  const backlink = () => parseOriginBacklink(props.originRef);

  return (
    <div class="content-meta">
      <Show when={props.origin === "github"}>
        <span class="origin-badge" title="Ingested from GitHub">
          ⎇ GitHub
        </span>
      </Show>
      <Show when={props.origin === "system"}>
        <span class="origin-badge origin-system" title="Posted by an automated integration">
          ⚙ system
        </span>
      </Show>
      <span class="author-name" classList={{ "is-self": author().isSelf }}>
        {author().label}
      </span>
      <time class="timestamp" title={props.createdAt.toISOString()}>
        {stamp(props.createdAt)}
      </time>
      <Show when={props.editedAt}>
        {(editedAt) => (
          <span class="edited-marker" title={`Edited ${editedAt().toISOString()}`}>
            (edited)
          </span>
        )}
      </Show>
      <Show when={backlink()}>
        {(href) => (
          <a class="origin-backlink" href={href()} target="_blank" rel="noopener noreferrer">
            view source ↗
          </a>
        )}
      </Show>
    </div>
  );
};

export default ContentMeta;
