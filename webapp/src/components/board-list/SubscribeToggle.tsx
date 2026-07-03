// Subscribe/mute toggle for a board (task C2, PLANDOC.md §7: board index
// "subscribe toggle", board header "subscribe/mute toggle"). One component
// covers both call sites — the board index list renders it without
// `showMute`, `BoardPage`'s header renders it with `showMute` — since the
// underlying op (SubscriptionService) is identical either way.
//
// Controlled: the parent owns the `Subscription | undefined` and re-renders
// via `onChange` after a successful subscribe/unsubscribe/set-muted call,
// same pattern `~/lib/session`'s `user`/`refresh` uses. This keeps a list of
// many toggles (the board index) to one shared subscriptions fetch instead
// of each row re-fetching its own.
import { A } from "@solidjs/router";
import { createSignal, Show, type Component } from "solid-js";
import type { Subscription, TargetType } from "~/gen/types.gen";
import { api } from "~/lib/api";
import { FirepitServiceError } from "~/lib/errors";
import { useSession } from "~/lib/session";
import "./board-list.css";

export interface SubscribeToggleProps {
  targetType: TargetType;
  targetId: string;
  /** The caller's current subscription to this target, or `undefined` if not subscribed. */
  subscription: Subscription | undefined;
  /** Called with the new subscription state after a successful subscribe/unsubscribe/mute call. */
  onChange: (next: Subscription | undefined) => void;
  /** Also render a mute checkbox once subscribed (BoardPage's header wants this; the board index list doesn't). */
  showMute?: boolean;
}

const SubscribeToggle: Component<SubscribeToggleProps> = (props) => {
  const session = useSession();
  const [pending, setPending] = createSignal(false);
  const [error, setError] = createSignal<string | null>(null);

  const toggleSubscribed = async (): Promise<void> => {
    setError(null);
    setPending(true);
    try {
      if (props.subscription) {
        await api.subscription.unsubscribe({ targetType: props.targetType, targetId: props.targetId });
        props.onChange(undefined);
      } else {
        const sub = await api.subscription.subscribe({ targetType: props.targetType, targetId: props.targetId });
        props.onChange(sub);
      }
    } catch (err) {
      setError(err instanceof FirepitServiceError ? err.message : "Couldn't update subscription.");
    } finally {
      setPending(false);
    }
  };

  const toggleMuted = async (): Promise<void> => {
    if (!props.subscription) return;
    setError(null);
    setPending(true);
    try {
      const next = await api.subscription.setMuted({
        targetType: props.targetType,
        targetId: props.targetId,
        muted: !props.subscription.muted,
      });
      props.onChange(next);
    } catch (err) {
      setError(err instanceof FirepitServiceError ? err.message : "Couldn't update mute.");
    } finally {
      setPending(false);
    }
  };

  return (
    <Show
      when={session.user()}
      fallback={
        <A href="/login" class="subscribe-toggle-login">
          Log in to subscribe
        </A>
      }
    >
      <span class="subscribe-toggle">
        <button
          type="button"
          class="subscribe-toggle-button"
          classList={{ subscribed: Boolean(props.subscription) }}
          disabled={pending()}
          onClick={() => void toggleSubscribed()}
        >
          {props.subscription ? "Subscribed" : "Subscribe"}
        </button>
        <Show when={props.showMute && props.subscription}>
          <label class="mute-toggle">
            <input
              type="checkbox"
              name="muted"
              checked={props.subscription?.muted ?? false}
              disabled={pending()}
              onChange={() => void toggleMuted()}
            />
            Muted
          </label>
        </Show>
        <Show when={error()}>
          <span class="form-error">{error()}</span>
        </Show>
      </span>
    </Show>
  );
};

export default SubscribeToggle;
