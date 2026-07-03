// Board post list (task C2, PLANDOC.md §7): activity-ordered, cursor-paged
// rows — title, author, comment count, last-activity relative time, unread
// indicator, and a small glyph for GitHub-origin posts. Used by
// `~/pages/BoardPage`.
import { A } from "@solidjs/router";
import { createEffect, createSignal, For, on, Show, type Accessor, type Component } from "solid-js";
import type { Post, UnreadSummary } from "~/gen/types.gen";
import { api } from "~/lib/api";
import { useSession } from "~/lib/session";
import { postIsUnread } from "~/lib/unread";
import { authorLabel } from "./authorLabel";
import "~/components/board-list/board-list.css";
import "./post-list.css";
import { relativeTime } from "./relativeTime";

const PAGE_SIZE = 20;

export interface PostListProps {
  boardId: string;
  boardSlug: string;
  /** Latest unread summary (from `~/lib/unread`'s poller) — read-only here. */
  summary: Accessor<UnreadSummary | null>;
}

const PostList: Component<PostListProps> = (props) => {
  const session = useSession();
  const [posts, setPosts] = createSignal<Post[]>([]);
  const [cursor, setCursor] = createSignal<string | undefined>(undefined);
  const [loading, setLoading] = createSignal(false);
  const [initialLoaded, setInitialLoaded] = createSignal(false);
  const [error, setError] = createSignal<string | null>(null);

  const load = async (reset: boolean): Promise<void> => {
    setLoading(true);
    setError(null);
    try {
      const page = await api.thread.listPosts({
        boardId: props.boardId,
        cursor: reset ? undefined : cursor(),
        limit: PAGE_SIZE,
      });
      setPosts((prev) => (reset ? page.posts : [...prev, ...page.posts]));
      setCursor(page.nextCursor);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
      setInitialLoaded(true);
    }
  };

  // Re-load from scratch whenever the board changes (navigating board -> board).
  createEffect(
    on(
      () => props.boardId,
      () => {
        setPosts([]);
        setCursor(undefined);
        setInitialLoaded(false);
        void load(true);
      },
    ),
  );

  return (
    <div class="post-list">
      <Show when={error()}>
        <p class="page-error">Couldn't load posts: {error()}</p>
      </Show>
      <Show when={initialLoaded() && posts().length === 0 && !error()}>
        <p class="rail-status">No posts yet — be the first.</p>
      </Show>
      <ul>
        <For each={posts()}>
          {(post) => {
            const unread = () => postIsUnread(props.summary(), post.id);
            return (
              <li classList={{ "post-row": true, unread: unread() }}>
                <A href={`/b/${props.boardSlug}/p/${post.id}`} class="post-row-title">
                  <Show when={unread()}>
                    <span class="unread-dot" aria-label="Unread" />
                  </Show>
                  {post.title}
                  <Show when={post.origin === "github"}>
                    <span class="origin-glyph" title="Posted from a GitHub event">
                      gh
                    </span>
                  </Show>
                </A>
                <p class="post-row-meta">
                  {authorLabel(post.authorId, session.user())} · {post.commentCount}{" "}
                  {post.commentCount === 1 ? "comment" : "comments"} · {relativeTime(post.lastActivityAt)}
                </p>
              </li>
            );
          }}
        </For>
      </ul>
      <Show when={cursor()}>
        <button type="button" class="link-button" disabled={loading()} onClick={() => void load(false)}>
          {loading() ? "Loading…" : "Load more"}
        </button>
      </Show>
    </div>
  );
};

export default PostList;
