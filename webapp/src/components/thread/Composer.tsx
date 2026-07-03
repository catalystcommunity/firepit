// Shared markdown composer (task C3 scope items 2 + 4): reply-at-any-depth,
// edit-in-place, and (via `showTitle`) post editing all go through this one
// component so the preview/mention/error behavior is identical everywhere
// it appears. The caller owns the actual RPC (`onSubmit`) and auth gating
// (nothing here calls `useSession` — `CommentNode`/`ThreadPage` only mount
// a `Composer` once the viewer is allowed to use it).
import { createMemo, createSignal, Show, type Component } from "solid-js";
import MarkdownBody from "./MarkdownBody";
import MentionAutocomplete from "./MentionAutocomplete";

export interface ComposerProps {
  initialBody?: string;
  initialTitle?: string;
  showTitle?: boolean;
  submitLabel: string;
  placeholder?: string;
  /** Handles seen elsewhere in the thread — see MentionAutocomplete's doc comment. */
  mentionCandidates: string[];
  onSubmit: (values: { title?: string; bodyMd: string }) => Promise<void>;
  onCancel?: () => void;
}

// The `@partial` token immediately before the caret, if any — e.g. typing
// "cc @bo|" (caret at `|`) matches "bo". No match when the caret isn't
// mid-token (a space or start-of-line follows the `@`... immediately, or
// there's no `@` at all before it).
function activeMentionQuery(value: string, caret: number): string | null {
  const before = value.slice(0, caret);
  const match = /@([a-zA-Z0-9_-]*)$/.exec(before);
  return match ? match[1] : null;
}

const Composer: Component<ComposerProps> = (props) => {
  const [title, setTitle] = createSignal(props.initialTitle ?? "");
  const [body, setBody] = createSignal(props.initialBody ?? "");
  const [preview, setPreview] = createSignal(false);
  const [submitting, setSubmitting] = createSignal(false);
  const [error, setError] = createSignal<string | null>(null);
  const [mentionQuery, setMentionQuery] = createSignal<string | null>(null);

  let textareaRef: HTMLTextAreaElement | undefined;

  const canSubmit = createMemo(() => {
    if (submitting()) return false;
    if (props.showTitle && title().trim().length === 0) return false;
    return body().trim().length > 0;
  });

  const onBodyInput = (e: InputEvent & { currentTarget: HTMLTextAreaElement }) => {
    const value = e.currentTarget.value;
    setBody(value);
    setMentionQuery(activeMentionQuery(value, e.currentTarget.selectionStart ?? value.length));
  };

  const pickMention = (handle: string) => {
    const ta = textareaRef;
    if (!ta) return;
    const caret = ta.selectionStart ?? body().length;
    const before = body().slice(0, caret);
    const after = body().slice(caret);
    const replaced = before.replace(/@([a-zA-Z0-9_-]*)$/, `@${handle} `);
    const next = replaced + after;
    setBody(next);
    setMentionQuery(null);
    queueMicrotask(() => {
      ta.focus();
      const pos = replaced.length;
      ta.selectionStart = ta.selectionEnd = pos;
    });
  };

  const submit = async (e: Event) => {
    e.preventDefault();
    if (!canSubmit()) return;
    setSubmitting(true);
    setError(null);
    try {
      await props.onSubmit({ title: props.showTitle ? title().trim() : undefined, bodyMd: body().trim() });
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <form class="composer" onSubmit={submit}>
      <Show when={props.showTitle}>
        <input
          class="composer-title"
          type="text"
          name="title"
          value={title()}
          onInput={(e) => setTitle(e.currentTarget.value)}
          placeholder="Title"
          aria-label="Title"
        />
      </Show>

      <div class="composer-tabs">
        <button type="button" class={preview() ? "" : "is-active"} onClick={() => setPreview(false)}>
          Write
        </button>
        <button type="button" class={preview() ? "is-active" : ""} onClick={() => setPreview(true)}>
          Preview
        </button>
      </div>

      <Show
        when={!preview()}
        fallback={<MarkdownBody class="md-body composer-preview" source={body() || "*Nothing to preview yet.*"} />}
      >
        <div class="composer-input-wrap">
          <textarea
            ref={textareaRef}
            class="composer-input"
            name="body"
            value={body()}
            onInput={onBodyInput}
            onKeyDown={(e) => {
              if ((e.metaKey || e.ctrlKey) && e.key === "Enter") void submit(e);
              if (e.key === "Escape") props.onCancel?.();
            }}
            placeholder={props.placeholder ?? "Write a reply… (markdown supported)"}
            rows={4}
          />
          <Show when={mentionQuery() !== null}>
            <MentionAutocomplete candidates={props.mentionCandidates} query={mentionQuery() ?? ""} onPick={pickMention} />
          </Show>
        </div>
      </Show>

      <Show when={error()}>{(msg) => <p class="form-error composer-error">{msg()}</p>}</Show>

      <div class="composer-actions">
        <Show when={props.onCancel}>
          <button type="button" class="btn-ghost" onClick={() => props.onCancel?.()} disabled={submitting()}>
            Cancel
          </button>
        </Show>
        <button type="submit" class="btn-primary" disabled={!canSubmit()}>
          {submitting() ? "Saving…" : props.submitLabel}
        </button>
      </div>
    </form>
  );
};

export default Composer;
