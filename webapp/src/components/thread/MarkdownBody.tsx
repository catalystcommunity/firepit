// Renders a post/comment body (or a composer preview) through the shared
// sanitizer (task C3 scope item 1). One tiny wrapper so nothing under
// components/thread ever touches `innerHTML` directly.
import { createMemo, type Component } from "solid-js";
import { renderMarkdown } from "~/lib/markdown";

export interface MarkdownBodyProps {
  source: string;
  class?: string;
}

const MarkdownBody: Component<MarkdownBodyProps> = (props) => {
  const html = createMemo(() => renderMarkdown(props.source));
  // eslint-disable-next-line solid/no-innerhtml -- the one sanctioned spot; see ~/lib/markdown.ts.
  return <div class={props.class ?? "md-body"} innerHTML={html()} />;
};

export default MarkdownBody;
