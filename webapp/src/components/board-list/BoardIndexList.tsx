// Board index (task C2, PLANDOC.md §7): every board, grouped announce vs.
// discussion (per `Board.kind`), each row showing title/description, a
// subscribe toggle, and an understated unread dot from the shared poller
// (`~/lib/unread`). Used by `~/pages/Home`.
import { A } from "@solidjs/router";
import { createResource, For, Show, type Component } from "solid-js";
import type { Board, Subscription } from "~/gen/types.gen";
import { api } from "~/lib/api";
import { useSession } from "~/lib/session";
import { boardUnreadCount, type UnreadPoller } from "~/lib/unread";
import SubscribeToggle from "./SubscribeToggle";
import "./board-list.css";

export interface BoardIndexListProps {
  poller: UnreadPoller;
}

const BoardIndexList: Component<BoardIndexListProps> = (props) => {
  const session = useSession();
  const [boardPage] = createResource(() => api.board.listBoards({}));
  // Only fetch the caller's own subscriptions once logged in — an
  // unauthenticated `list-subscriptions` call is a guaranteed
  // FirepitServiceError (Unauthenticated), same as `whoami`.
  const [subsResource, { mutate: mutateSubs }] = createResource(
    () => (session.user() ? session.user()!.id : undefined),
    () => api.subscription.listSubscriptions({}),
  );

  const subscriptionFor = (boardId: string): Subscription | undefined =>
    subsResource()?.subscriptions.find((s) => s.targetType === "board" && s.targetId === boardId);

  const updateSubscription = (boardId: string, next: Subscription | undefined): void => {
    mutateSubs((prev) => {
      const rest = prev?.subscriptions.filter((s) => !(s.targetType === "board" && s.targetId === boardId)) ?? [];
      return { subscriptions: next ? [...rest, next] : rest };
    });
  };

  const announceBoards = (): Board[] => (boardPage()?.boards ?? []).filter((b) => b.kind === "announce");
  const discussionBoards = (): Board[] => (boardPage()?.boards ?? []).filter((b) => b.kind === "discussion");

  const Row: Component<{ board: Board }> = (rowProps) => (
    <li class="board-row">
      <div class="board-row-main">
        <A href={`/b/${rowProps.board.slug}`} class="board-row-title">
          <Show when={boardUnreadCount(props.poller.summary(), rowProps.board.id) > 0}>
            <span class="unread-dot" aria-label="Unread activity" />
          </Show>
          {rowProps.board.title}
        </A>
        <Show when={rowProps.board.description}>
          <p class="board-row-desc">{rowProps.board.description}</p>
        </Show>
      </div>
      <SubscribeToggle
        targetType="board"
        targetId={rowProps.board.id}
        subscription={subscriptionFor(rowProps.board.id)}
        onChange={(next) => updateSubscription(rowProps.board.id, next)}
      />
    </li>
  );

  return (
    <div class="board-index">
      <Show when={announceBoards().length > 0}>
        <section class="board-group">
          <h3>Announcements</h3>
          <ul>
            <For each={announceBoards()}>{(board) => <Row board={board} />}</For>
          </ul>
        </section>
      </Show>
      <Show when={discussionBoards().length > 0}>
        <section class="board-group">
          <h3>Discussion</h3>
          <ul>
            <For each={discussionBoards()}>{(board) => <Row board={board} />}</For>
          </ul>
        </section>
      </Show>
      <Show when={(boardPage()?.boards.length ?? 0) === 0}>
        <p class="rail-status">No boards yet.</p>
      </Show>
    </div>
  );
};

export default BoardIndexList;
