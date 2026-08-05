import { A } from "@solidjs/router";
import { createEffect, createSignal, For, on, Show, type Accessor, type Component } from "solid-js";
import type { Category, Post, UnreadSummary } from "~/gen/types.gen";
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
  categories?: readonly Category[];
}

const PostList: Component<PostListProps> = (props) => {
  const session = useSession();
  const [posts, setPosts] = createSignal<Post[]>([]);
  const [cursor, setCursor] = createSignal<string | undefined>(undefined);
  const [loading, setLoading] = createSignal(false);
  const [initialLoaded, setInitialLoaded] = createSignal(false);
  const [error, setError] = createSignal<string | null>(null);
  const [selectedCategoryIds, setSelectedCategoryIds] = createSignal<string[]>([]);
  const [includeUncategorized, setIncludeUncategorized] = createSignal(false);

  const toggleCategory = (id: string): void => {
    setSelectedCategoryIds((current) => (current.includes(id) ? current.filter((item) => item !== id) : [...current, id]));
  };

  const load = async (reset: boolean): Promise<void> => {
    setLoading(true);
    setError(null);
    try {
      const page = await api.thread.listPosts({
        boardId: props.boardId,
        categoryIds: selectedCategoryIds().length > 0 ? selectedCategoryIds() : includeUncategorized() ? [] : undefined,
        includeUncategorized: includeUncategorized() ? true : selectedCategoryIds().length > 0 ? false : undefined,
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
      () => [props.boardId, selectedCategoryIds().join(","), includeUncategorized()] as const,
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
      <div class="post-list-heading">
        <h2>Threads</h2>
        <p>Ordered by latest activity.</p>
      </div>
      <Show when={(props.categories?.length ?? 0) > 0}>
        <fieldset class="category-filter">
          <legend>Filter categories</legend>
          <For each={props.categories ?? []}>
            {(category) => (
              <label>
                <input
                  type="checkbox"
                  checked={selectedCategoryIds().includes(category.id)}
                  onChange={() => toggleCategory(category.id)}
                />
                {category.name}
              </label>
            )}
          </For>
          <label>
            <input
              type="checkbox"
              checked={includeUncategorized()}
              onChange={(event) => setIncludeUncategorized(event.currentTarget.checked)}
            />
            Uncategorized
          </label>
        </fieldset>
      </Show>
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
                  <span>{authorLabel(post.authorId, post.authorHandle, session.user())}</span>
                  <span>
                    {post.commentCount} {post.commentCount === 1 ? "comment" : "comments"}
                  </span>
                  <span>{relativeTime(post.lastActivityAt)}</span>
                </p>
                <div class="post-categories">
                  <Show when={post.categoryIds.length > 0} fallback={<span>Uncategorized</span>}>
                    <For each={post.categoryIds}>
                      {(id) => <span>{props.categories?.find((category) => category.id === id)?.name ?? id}</span>}
                    </For>
                  </Show>
                </div>
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
