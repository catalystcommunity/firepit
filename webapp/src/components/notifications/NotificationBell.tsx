// The app shell's notification bell (task C4, PLANDOC.md §7 — replaces
// AppShell.tsx's C1 placeholder `<A class="bell">`). Polls for unread
// count (60s + on focus, via src/lib/notifications.ts's poller), and opens
// a small dropdown of the latest few notifications with a "view all" link.
// Understated on purpose: a quiet dot/count, not a screaming red badge —
// see index.css's `.notif-dot` (a bordered pill in the app's neutral
// palette, not `--danger`).
import { A, useNavigate } from "@solidjs/router";
import { createSignal, For, onCleanup, onMount, Show, type Component } from "solid-js";
import type { Notification } from "~/gen/types.gen";
import { api } from "~/lib/api";
import { createNotificationsPoller, createPostSummaryResolver, notificationHref } from "~/lib/notifications";
import { useSession } from "~/lib/session";
import NotificationRow from "./NotificationRow";

const NotificationBell: Component = () => {
  const session = useSession();
  const navigate = useNavigate();
  const poller = createNotificationsPoller(() => !!session.user());
  const resolver = createPostSummaryResolver();
  const [open, setOpen] = createSignal(false);
  let rootEl: HTMLDivElement | undefined;

  const close = () => setOpen(false);

  const handleDocClick = (e: MouseEvent) => {
    if (open() && rootEl && !rootEl.contains(e.target as Node)) close();
  };
  const handleKey = (e: KeyboardEvent) => {
    if (e.key === "Escape") close();
  };

  onMount(() => {
    document.addEventListener("click", handleDocClick);
    document.addEventListener("keydown", handleKey);
  });
  onCleanup(() => {
    document.removeEventListener("click", handleDocClick);
    document.removeEventListener("keydown", handleKey);
  });

  const toggle = () => {
    const next = !open();
    setOpen(next);
    if (next) void poller.refresh();
  };

  const select = async (n: Notification): Promise<void> => {
    close();
    if (!n.readAt) {
      try {
        await api.notification.markNotificationRead([n.id]);
      } catch {
        // Best-effort: navigation still proceeds even if the mark-read call fails — the
        // user is reading it right now either way, and NotificationsPage lets them retry.
      }
      void poller.refresh();
    }
    const summary = await resolver.resolve(n.postId);
    navigate(notificationHref(n, summary));
  };

  const countLabel = () => (poller.saturated() ? `${poller.unreadCount()}+` : `${poller.unreadCount()}`);

  return (
    <div class="notif-bell" ref={rootEl}>
      <button
        type="button"
        class="bell"
        aria-haspopup="true"
        aria-expanded={open()}
        aria-label={poller.unreadCount() > 0 ? `Notifications — ${countLabel()} unread` : "Notifications"}
        title="Notifications"
        onClick={toggle}
      >
        <span aria-hidden="true">🔔</span>
        <Show when={poller.unreadCount() > 0}>
          <span class="notif-dot">{countLabel()}</span>
        </Show>
      </button>
      <Show when={open()}>
        <div class="notif-dropdown" role="menu">
          <Show when={poller.latest().length > 0} fallback={<p class="notif-empty">No notifications yet.</p>}>
            <ul>
              <For each={poller.latest()}>
                {(n) => (
                  <NotificationRow notification={n} viewerId={session.user()?.id} compact onSelect={() => void select(n)} />
                )}
              </For>
            </ul>
          </Show>
          <A href="/notifications" class="notif-view-all" onClick={close}>
            View all
          </A>
        </div>
      </Show>
    </div>
  );
};

export default NotificationBell;
