// /b/:slug (task C2, PLANDOC.md §7): board header (title/description,
// subscribe/mute toggle), the new-post composer, and the activity-ordered
// post list with cursor "load more".
import { useParams } from "@solidjs/router";
import { createMemo, createResource, Show, type Component } from "solid-js";
import PostComposer from "~/components/composer/PostComposer";
import PostList from "~/components/post-list/PostList";
import SubscribeToggle from "~/components/board-list/SubscribeToggle";
import type { Subscription } from "~/gen/types.gen";
import { api } from "~/lib/api";
import { useSession } from "~/lib/session";
import { startUnreadPoller } from "~/lib/unread";
import "./BoardPage.css";

const BoardPage: Component = () => {
  const params = useParams();
  const session = useSession();
  const poller = startUnreadPoller(() => session.user() !== null);

  const [board] = createResource(() => params.slug, (slug) => api.board.getBoard(slug));
  const [subsResource, { mutate: mutateSubs }] = createResource(
    () => (session.user() && board() ? board()!.id : undefined),
    () => api.subscription.listSubscriptions({}),
  );

  const subscription = createMemo<Subscription | undefined>(() =>
    subsResource()?.subscriptions.find((s) => s.targetType === "board" && s.targetId === board()?.id),
  );

  const updateSubscription = (next: Subscription | undefined): void => {
    const boardId = board()?.id;
    if (!boardId) return;
    mutateSubs((prev) => {
      const rest = prev?.subscriptions.filter((s) => !(s.targetType === "board" && s.targetId === boardId)) ?? [];
      return { subscriptions: next ? [...rest, next] : rest };
    });
  };

  return (
    <Show when={board()} fallback={<p class="page-status">Loading board…</p>} keyed>
      {(b) => (
        <section class="board-page">
          <header class="board-header">
            <div>
              <p class="eyebrow">/{b.slug}</p>
              <h1>{b.title}</h1>
              <Show when={b.description}>
                <p class="board-header-desc">{b.description}</p>
              </Show>
            </div>
            <SubscribeToggle
              targetType="board"
              targetId={b.id}
              subscription={subscription()}
              onChange={updateSubscription}
              showMute
            />
          </header>

          <section class="board-composer-section" aria-label="Start a new thread">
            <div>
              <h2>Start a thread</h2>
              <p>Use a specific title and enough context that future readers can join in.</p>
            </div>
            <PostComposer boardId={b.id} boardSlug={b.slug} />
          </section>

          <PostList boardId={b.id} boardSlug={b.slug} summary={poller.summary} />
        </section>
      )}
    </Show>
  );
};

export default BoardPage;
