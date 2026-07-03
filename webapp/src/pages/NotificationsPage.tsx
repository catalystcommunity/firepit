// /notifications (task C4, PLANDOC.md §7): the cursor-paginated inbox,
// newest first. Clicking a row marks it read and navigates to the thread
// (a permalink to the comment when the notification targets one); rows
// also offer an explicit "Mark read" action, and there's a page-level
// "Mark all read". Subscriptions management lives on /settings instead of
// here — see SettingsPage.tsx's doc comment for why.
//
// There is no mark-*unread* op for notifications in CSIL (NotificationService
// only has mark-notification-read/mark-all-read — ReadService's
// mark-unread is a different thing, for posts/comments), so "mark
// read/unread per row where the API allows" only ever offers the read
// half here.
import { useNavigate } from "@solidjs/router";
import { createSignal, For, onMount, Show, type Component } from "solid-js";
import NotificationRow from "~/components/notifications/NotificationRow";
import { api } from "~/lib/api";
import { FirepitServiceError } from "~/lib/errors";
import { createPostSummaryResolver, notificationHref, type PostSummary } from "~/lib/notifications";
import { useSession } from "~/lib/session";
import type { Notification } from "~/gen/types.gen";

const PAGE_SIZE = 5;

const NotificationsPage: Component = () => {
  const session = useSession();
  const navigate = useNavigate();
  const resolver = createPostSummaryResolver();

  const [items, setItems] = createSignal<Notification[]>([]);
  const [summaries, setSummaries] = createSignal<Record<string, PostSummary>>({});
  const [cursor, setCursor] = createSignal<string | undefined>(undefined);
  const [hasMore, setHasMore] = createSignal(false);
  const [loading, setLoading] = createSignal(true);
  const [loadingMore, setLoadingMore] = createSignal(false);
  const [markingAll, setMarkingAll] = createSignal(false);
  const [error, setError] = createSignal<string | null>(null);

  const describe = (err: unknown, fallback: string): string =>
    err instanceof FirepitServiceError ? err.message : fallback;

  const resolveSummaries = (notifications: readonly Notification[]): void => {
    for (const postId of new Set(notifications.map((n) => n.postId))) {
      void resolver.resolve(postId).then((summary) => setSummaries((prev) => ({ ...prev, [postId]: summary })));
    }
  };

  const loadFirstPage = async (): Promise<void> => {
    setLoading(true);
    setError(null);
    try {
      const page = await api.notification.listNotifications({ limit: PAGE_SIZE });
      setItems(page.notifications);
      setCursor(page.nextCursor);
      setHasMore(!!page.nextCursor);
      resolveSummaries(page.notifications);
    } catch (err) {
      setError(describe(err, "Couldn't load notifications."));
    } finally {
      setLoading(false);
    }
  };

  onMount(() => void loadFirstPage());

  const loadMore = async (): Promise<void> => {
    const after = cursor();
    if (!after) return;
    setLoadingMore(true);
    setError(null);
    try {
      const page = await api.notification.listNotifications({ limit: PAGE_SIZE, cursor: after });
      setItems((prev) => [...prev, ...page.notifications]);
      setCursor(page.nextCursor);
      setHasMore(!!page.nextCursor);
      resolveSummaries(page.notifications);
    } catch (err) {
      setError(describe(err, "Couldn't load more notifications."));
    } finally {
      setLoadingMore(false);
    }
  };

  const markRead = async (n: Notification): Promise<void> => {
    if (n.readAt) return;
    const prev = items();
    setItems((cur) => cur.map((x) => (x.id === n.id ? { ...x, readAt: new Date() } : x)));
    try {
      await api.notification.markNotificationRead([n.id]);
    } catch (err) {
      setItems(prev);
      setError(describe(err, "Couldn't mark that notification read."));
    }
  };

  const markAllRead = async (): Promise<void> => {
    const prev = items();
    setMarkingAll(true);
    setError(null);
    setItems((cur) => cur.map((x) => ({ ...x, readAt: x.readAt ?? new Date() })));
    try {
      await api.notification.markAllRead({});
    } catch (err) {
      setItems(prev);
      setError(describe(err, "Couldn't mark everything read."));
    } finally {
      setMarkingAll(false);
    }
  };

  const open = async (n: Notification): Promise<void> => {
    void markRead(n);
    const summary = summaries()[n.postId] ?? (await resolver.resolve(n.postId));
    navigate(notificationHref(n, summary));
  };

  const anyUnread = () => items().some((n) => !n.readAt);

  return (
    <section class="notifications-page">
      <header class="notifications-header">
        <h2>Notifications</h2>
        <button type="button" disabled={!anyUnread() || markingAll()} onClick={() => void markAllRead()}>
          Mark all read
        </button>
      </header>

      <Show when={error()}>
        <p class="form-error" role="alert">
          {error()}
        </p>
      </Show>

      <Show when={!loading()} fallback={<p class="page-status">Loading…</p>}>
        <Show
          when={items().length > 0}
          fallback={<p class="rail-status">No notifications yet — subscribe to a board or post to start getting them.</p>}
        >
          <ul class="notif-list">
            <For each={items()}>
              {(n) => (
                <NotificationRow
                  notification={n}
                  summary={summaries()[n.postId]}
                  viewerId={session.user()?.id}
                  onSelect={() => void open(n)}
                  onMarkRead={n.readAt ? undefined : () => void markRead(n)}
                />
              )}
            </For>
          </ul>
          <Show when={hasMore()}>
            <button type="button" class="link-button" disabled={loadingMore()} onClick={() => void loadMore()}>
              {loadingMore() ? "Loading…" : "Load more"}
            </button>
          </Show>
        </Show>
      </Show>
    </section>
  );
};

export default NotificationsPage;
