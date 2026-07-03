// One notification row — shared between the AppShell bell's dropdown
// preview (compact, no per-row mark-read) and the full /notifications inbox
// (task C4, PLANDOC.md §7). Read rows render visibly quiet (dimmed, no
// unread dot/weight) rather than as a competing "look at me" state — design
// direction: notifications are a curated signal channel, not an engagement
// surface.
import { Show, type Component } from "solid-js";
import type { Notification } from "~/gen/types.gen";
import { NOTIFICATION_GLYPH, NOTIFICATION_LABEL, actorLabel, relativeTime, type PostSummary } from "~/lib/notifications";

export interface NotificationRowProps {
  notification: Notification;
  /** Resolved post title, when the caller has bothered to fetch one (the full inbox does;
   * the bell's compact preview skips it to stay a single cheap poll). */
  summary?: PostSummary;
  viewerId?: string;
  compact?: boolean;
  onSelect: () => void;
  /** Omitted entirely for an already-read row, or when the caller (the bell) doesn't offer
   * per-row mark-read at all. There is no mark-*unread* op at notification granularity in
   * CSIL (NotificationService only has mark-notification-read/mark-all-read — unlike
   * ReadService's post/comment mark-unread) so that half of "mark-read/unread per row where
   * the API allows" is simply not offered here; see NotificationsPage.tsx's doc comment. */
  onMarkRead?: () => void;
}

const NotificationRow: Component<NotificationRowProps> = (props) => (
  <li classList={{ "notif-row-item": true, unread: !props.notification.readAt, compact: !!props.compact }}>
    <button type="button" class="notif-row" onClick={() => props.onSelect()}>
      <span class="notif-glyph" aria-hidden="true">
        {NOTIFICATION_GLYPH[props.notification.event]}
      </span>
      <span class="notif-text">
        <span class="notif-label">
          {NOTIFICATION_LABEL[props.notification.event]} · {actorLabel(props.notification.actorId, props.viewerId)}
        </span>
        <Show when={props.summary}>
          <span class="notif-target">{props.summary?.unresolved ? "(post unavailable)" : props.summary?.title}</span>
        </Show>
      </span>
      <span class="notif-time">{relativeTime(props.notification.createdAt)}</span>
    </button>
    <Show when={!props.notification.readAt && props.onMarkRead}>
      <button type="button" class="notif-mark-read" onClick={() => props.onMarkRead?.()}>
        Mark read
      </button>
    </Show>
  </li>
);

export default NotificationRow;
