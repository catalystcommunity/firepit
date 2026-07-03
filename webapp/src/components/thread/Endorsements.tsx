// Inline named endorsement list + endorse/retract (task C3 scope item 3,
// PLANDOC.md §1/§4/§9.3). Renders exactly the order `list-endorsements`
// returns — SERVER order, never re-sorted here: the ordering is per-viewer
// (friends first, then reputation) and is meaningless to re-derive
// client-side. Optimistic endorse/retract with rollback on error.
//
// `GetThreadRequest`'s `Thread` has no embedded endorsement data (CSIL's
// `Thread` type is just `{ post, comments }` — see types.gen.ts), so this
// component does its own `list-endorsements` fetch per target; a thread with
// N items makes N of these. That's the API CSIL currently offers (no
// batch/thread-wide endorsement listing op) — a reasonable follow-up if it
// ever shows up as a real perf problem.
import { createResource, createSignal, For, Show, type Component } from "solid-js";
import { api } from "~/lib/api";
import type { Endorsement, TargetType, UserProfile } from "~/gen/types.gen";
import { describeAuthor } from "./identity";

export interface EndorsementsProps {
  targetType: TargetType;
  targetId: string;
  /** The content's author — endorsing your own content is blocked (server-enforced too). */
  authorId: string;
  /** Deleted content can't be endorsed. */
  isDeleted: boolean;
  viewer: UserProfile | null;
}

const Endorsements: Component<EndorsementsProps> = (props) => {
  const [resource, { mutate, refetch }] = createResource(
    () => ({ targetType: props.targetType, targetId: props.targetId }),
    (target) => api.endorsement.listEndorsements(target),
  );
  const [pending, setPending] = createSignal(false);
  const [error, setError] = createSignal<string | null>(null);

  const list = () => resource()?.endorsements ?? [];
  const viewerEndorsement = () => list().find((e) => e.userId === props.viewer?.id);
  const eligible = () =>
    !!props.viewer && !props.isDeleted && props.authorId !== props.viewer.id;

  const toggle = async () => {
    if (pending() || !props.viewer) return;
    setPending(true);
    setError(null);
    const target = { targetType: props.targetType, targetId: props.targetId };
    const existing = viewerEndorsement();
    const before = list();

    if (existing) {
      // Optimistic retract: drop it, preserving every other entry's order.
      mutate({ endorsements: before.filter((e) => e.id !== existing.id) });
      try {
        await api.endorsement.retract(target);
      } catch (err) {
        mutate({ endorsements: before }); // rollback
        setError(err instanceof Error ? err.message : String(err));
      } finally {
        setPending(false);
      }
      return;
    }

    // Optimistic endorse: the mock/server appends new endorsements at the
    // end of the target's list (see store.ts's `endorse`) — mirror that so
    // the optimistic state matches what a refetch would show.
    const optimistic: Endorsement = {
      id: `optimistic-${Date.now()}`,
      userId: props.viewer.id,
      authorHandle: props.viewer.handle,
      targetType: props.targetType,
      targetId: props.targetId,
      createdAt: new Date(),
    };
    mutate({ endorsements: [...before, optimistic] });
    try {
      const real = await api.endorsement.endorse(target);
      mutate({ endorsements: [...before, real] });
    } catch (err) {
      mutate({ endorsements: before }); // rollback
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setPending(false);
    }
  };

  return (
    <div class="endorsements">
      <Show when={list().length > 0}>
        <span class="endorsement-list">
          Endorsed by{" "}
          <For each={list()}>
            {(e, i) => (
              <>
                <span class="endorser">
                  {describeAuthor(e.userId, e.authorHandle, "user", props.viewer).label}
                  <Show when={e.roleBadge}>{(badge) => <span class="role-badge">{badge()}</span>}</Show>
                </span>
                {i() < list().length - 1 ? ", " : ""}
              </>
            )}
          </For>
        </span>
      </Show>

      <Show when={eligible()}>
        <button
          type="button"
          class={viewerEndorsement() ? "endorse-btn is-endorsed" : "endorse-btn"}
          disabled={pending() || resource.loading}
          onClick={() => void toggle()}
        >
          {viewerEndorsement() ? "Retract endorsement" : "Endorse"}
        </button>
      </Show>

      <Show when={error()}>
        {(msg) => (
          <span class="form-error endorsement-error">
            {msg()}{" "}
            <button type="button" class="link-button" onClick={() => void refetch()}>
              retry
            </button>
          </span>
        )}
      </Show>
    </div>
  );
};

export default Endorsements;
