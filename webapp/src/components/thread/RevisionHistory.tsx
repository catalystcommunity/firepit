// Collapsible edit-history panel (task C3 scope item 4: "revision history
// view (list-revisions) in a collapsible panel or modal"). Lazily fetches
// on first expand — most items are never edited, so most CommentNode/
// PostView instances should never issue this call at all.
import { createResource, createSignal, For, Show, type Component } from "solid-js";
import { api } from "~/lib/api";
import type { TargetType } from "~/gen/types.gen";
import MarkdownBody from "./MarkdownBody";

export interface RevisionHistoryProps {
  targetType: TargetType;
  targetId: string;
}

const stamp = (d: Date): string => d.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });

const RevisionHistory: Component<RevisionHistoryProps> = (props) => {
  const [open, setOpen] = createSignal(false);
  const [resource] = createResource(
    () => (open() ? { targetType: props.targetType, targetId: props.targetId } : null),
    (target) => api.thread.listRevisions(target),
  );

  return (
    <div class="revision-history">
      <button type="button" class="link-button revision-toggle" onClick={() => setOpen((v) => !v)}>
        {open() ? "Hide edit history" : "View edit history"}
      </button>
      <Show when={open()}>
        <div class="revision-panel" role="region" aria-label="Edit history">
          <Show when={resource.loading}>
            <p class="page-status">Loading history…</p>
          </Show>
          <Show when={resource()}>
            {(list) => (
              <Show
                when={list().revisions.length > 0}
                fallback={<p class="page-status">No prior revisions.</p>}
              >
                <ol class="revision-list">
                  <For each={list().revisions}>
                    {(rev) => (
                      <li class="revision-entry">
                        <div class="revision-meta">
                          <time>{stamp(rev.createdAt)}</time>
                        </div>
                        <Show when={rev.prevTitle}>{(t) => <p class="revision-title">{t()}</p>}</Show>
                        <MarkdownBody class="md-body revision-body" source={rev.prevBodyMd} />
                      </li>
                    )}
                  </For>
                </ol>
              </Show>
            )}
          </Show>
        </div>
      </Show>
    </div>
  );
};

export default RevisionHistory;
