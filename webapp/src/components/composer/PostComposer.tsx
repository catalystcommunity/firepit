// New-post composer (task C2, PLANDOC.md §7): title + markdown body with a
// write/preview tab, submit via ThreadService.create-post, optimistic
// navigation to the new thread route on success. Auth-gated: create-post
// requires a session (see FixtureStore.createPost's requireAuth(), and the
// real ThreadService will too), so an anonymous visitor gets a login prompt
// instead of the form. Used by `~/pages/BoardPage`.
import { A, useNavigate } from "@solidjs/router";
import { createSignal, Show, type Component } from "solid-js";
import type { Post } from "~/gen/types.gen";
import { api } from "~/lib/api";
import { FirepitServiceError } from "~/lib/errors";
import { renderMarkdown } from "~/lib/markdown";
import { useSession } from "~/lib/session";
import "./composer.css";

export interface PostComposerProps {
  boardId: string;
  boardSlug: string;
  /** Called with the created post right before navigating to its thread — lets the board page optimistically prepend it to its list. */
  onCreated?: (post: Post) => void;
}

type Tab = "write" | "preview";

const TITLE_MAX = 512;
const BODY_MAX = 100_000;

interface FieldError {
  /** Which field this belongs to (matches `ServiceError.field`'s naming — "title"/"bodyMd"); absent for a general/non-field error. */
  field?: string;
  message: string;
}

const PostComposer: Component<PostComposerProps> = (props) => {
  const session = useSession();
  const navigate = useNavigate();
  const [tab, setTab] = createSignal<Tab>("write");
  const [title, setTitle] = createSignal("");
  const [bodyMd, setBodyMd] = createSignal("");
  const [submitting, setSubmitting] = createSignal(false);
  const [fieldError, setFieldError] = createSignal<FieldError | null>(null);

  const validate = (): FieldError | null => {
    const trimmedTitle = title().trim();
    const trimmedBody = bodyMd().trim();
    if (trimmedTitle.length === 0) return { field: "title", message: "Title is required." };
    if (trimmedTitle.length > TITLE_MAX) {
      return { field: "title", message: `Title must be ${TITLE_MAX} characters or fewer.` };
    }
    if (trimmedBody.length === 0) return { field: "bodyMd", message: "Body is required." };
    if (trimmedBody.length > BODY_MAX) {
      return { field: "bodyMd", message: `Body must be ${BODY_MAX} characters or fewer.` };
    }
    return null;
  };

  const submit = async (e: Event): Promise<void> => {
    e.preventDefault();
    const validationError = validate();
    if (validationError) {
      setFieldError(validationError);
      return;
    }
    setFieldError(null);
    setSubmitting(true);
    try {
      const post = await api.thread.createPost({
        boardId: props.boardId,
        title: title().trim(),
        bodyMd: bodyMd().trim(),
      });
      props.onCreated?.(post);
      navigate(`/b/${props.boardSlug}/p/${post.id}`);
    } catch (err) {
      if (err instanceof FirepitServiceError) {
        setFieldError({ field: err.field, message: err.message });
      } else {
        setFieldError({ message: err instanceof Error ? err.message : "Couldn't create the post." });
      }
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Show
      when={session.user()}
      fallback={
        <p class="composer-login-prompt">
          <A href="/login">Log in</A> to start a new thread on this board.
        </p>
      }
    >
      <form class="composer" onSubmit={(e) => void submit(e)}>
        <label class="composer-field">
          <span>Thread title</span>
          <input
            type="text"
            name="title"
            value={title()}
            onInput={(e) => setTitle(e.currentTarget.value)}
            maxLength={TITLE_MAX}
            placeholder="What should the project decide, fix, or discuss?"
            aria-invalid={fieldError()?.field === "title"}
          />
          <Show when={fieldError()?.field === "title"}>
            <span class="form-error">{fieldError()?.message}</span>
          </Show>
        </label>

        <div class="composer-tabs" role="tablist">
          <button
            type="button"
            role="tab"
            aria-selected={tab() === "write"}
            classList={{ active: tab() === "write" }}
            onClick={() => setTab("write")}
          >
            Write
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={tab() === "preview"}
            classList={{ active: tab() === "preview" }}
            onClick={() => setTab("preview")}
          >
            Preview
          </button>
        </div>

        <Show
          when={tab() === "write"}
          fallback={
            // renderMarkdown (~/lib/markdown.ts) runs every body through DOMPurify with a tight tag/attr
            // allowlist before this ever renders; see that module's doc comment for the full
            // sanitization posture (mirrors longhouse's MarkdownEditor).
            // eslint-disable-next-line solid/no-innerhtml
            <div class="composer-preview md-body" innerHTML={renderMarkdown(bodyMd())} />
          }
        >
          <textarea
            class="composer-body"
            name="bodyMd"
            rows={10}
            value={bodyMd()}
            onInput={(e) => setBodyMd(e.currentTarget.value)}
            placeholder="Add background, links, decisions needed, or what you already tried. Markdown is supported."
            aria-invalid={fieldError()?.field === "bodyMd"}
          />
        </Show>
        <Show when={fieldError()?.field === "bodyMd"}>
          <span class="form-error">{fieldError()?.message}</span>
        </Show>
        <Show when={fieldError() && !fieldError()?.field}>
          <p class="form-error">{fieldError()?.message}</p>
        </Show>

        <button type="submit" disabled={submitting()}>
          {submitting() ? "Posting…" : "Start thread"}
        </button>
      </form>
    </Show>
  );
};

export default PostComposer;
