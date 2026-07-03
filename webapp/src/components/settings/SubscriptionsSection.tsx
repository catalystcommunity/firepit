// /settings' subscriptions-management section (task C4, PLANDOC.md §7 —
// "subscriptions management page (incl. mutes)"). Landed as a section on
// /settings rather than a tab on /notifications: settings is already where
// "how I manage what I follow" lives (mention policy, grants, friend
// groups), and it keeps /notifications focused purely on the inbox. See
// SettingsPage.tsx's doc comment for the same note.
//
// Grouped by target_type (board/post/comment) with titles resolved via
// src/lib/notifications.ts's post/board resolver. Comment-target
// subscriptions are the one gap: CSIL has no "which post is this comment
// under" op (ThreadService only offers get-thread by post id), so there is
// no reliable way to resolve a bare comment id into a post title/board —
// shown as a plain id rather than guessing.
import { createSignal, For, onMount, Show, type Component } from "solid-js";
import type { Subscription, TargetType } from "~/gen/types.gen";
import { api } from "~/lib/api";
import { FirepitServiceError } from "~/lib/errors";
import { createPostSummaryResolver } from "~/lib/notifications";

const GROUP_ORDER: readonly TargetType[] = ["board", "post", "comment"];
const GROUP_TITLES: Record<TargetType, string> = {
  board: "Boards",
  post: "Posts",
  comment: "Comment threads",
};

const SubscriptionsSection: Component = () => {
  const resolver = createPostSummaryResolver();
  const [subs, setSubs] = createSignal<Subscription[]>([]);
  const [labels, setLabels] = createSignal<Record<string, string>>({});
  const [loading, setLoading] = createSignal(true);
  const [error, setError] = createSignal<string | null>(null);
  const [busyId, setBusyId] = createSignal<string | null>(null);

  const describe = (err: unknown, fallback: string): string =>
    err instanceof FirepitServiceError ? err.message : fallback;

  const labelKey = (s: Subscription): string => `${s.targetType}:${s.targetId}`;

  const resolveLabel = async (s: Subscription): Promise<void> => {
    if (s.targetType === "board") {
      try {
        const boards = await api.board.listBoards({});
        const board = boards.boards.find((b) => b.id === s.targetId);
        setLabels((cur) => ({ ...cur, [labelKey(s)]: board?.title ?? s.targetId }));
      } catch {
        setLabels((cur) => ({ ...cur, [labelKey(s)]: s.targetId }));
      }
      return;
    }
    if (s.targetType === "post") {
      const summary = await resolver.resolve(s.targetId);
      setLabels((cur) => ({ ...cur, [labelKey(s)]: summary.unresolved ? "(post unavailable)" : summary.title }));
      return;
    }
    setLabels((cur) => ({ ...cur, [labelKey(s)]: `Comment ${s.targetId.slice(0, 10)}…` }));
  };

  const load = async (): Promise<void> => {
    try {
      const list = await api.subscription.listSubscriptions({});
      setSubs(list.subscriptions);
      for (const s of list.subscriptions) void resolveLabel(s);
    } catch (err) {
      setError(describe(err, "Couldn't load subscriptions."));
    } finally {
      setLoading(false);
    }
  };

  onMount(() => void load());

  const toggleMute = async (s: Subscription): Promise<void> => {
    const prev = subs();
    setBusyId(s.id);
    setError(null);
    setSubs((cur) => cur.map((x) => (x.id === s.id ? { ...x, muted: !x.muted } : x)));
    try {
      await api.subscription.setMuted({ targetType: s.targetType, targetId: s.targetId, muted: !s.muted });
    } catch (err) {
      setSubs(prev);
      setError(describe(err, "Couldn't update that subscription."));
    } finally {
      setBusyId(null);
    }
  };

  const unsubscribe = async (s: Subscription): Promise<void> => {
    const prev = subs();
    setBusyId(s.id);
    setError(null);
    setSubs((cur) => cur.filter((x) => x.id !== s.id));
    try {
      await api.subscription.unsubscribe({ targetType: s.targetType, targetId: s.targetId });
    } catch (err) {
      setSubs(prev);
      setError(describe(err, "Couldn't unsubscribe."));
    } finally {
      setBusyId(null);
    }
  };

  const grouped = () => {
    const groups: Record<TargetType, Subscription[]> = { board: [], post: [], comment: [] };
    for (const s of subs()) groups[s.targetType].push(s);
    return groups;
  };

  return (
    <section class="settings-section">
      <h3>Subscriptions</h3>
      <p>
        Everything you're subscribed to. Muting keeps the subscription without its notifications — handy for
        carving one noisy post out of a board you otherwise follow.
      </p>
      <Show when={error()}>
        <p class="form-error" role="alert">
          {error()}
        </p>
      </Show>
      <Show when={!loading()} fallback={<p class="page-status">Loading…</p>}>
        <Show when={subs().length > 0} fallback={<p class="rail-status">No subscriptions yet.</p>}>
          <For each={GROUP_ORDER.filter((t) => grouped()[t].length > 0)}>
            {(type) => (
              <div class="subscription-group">
                <h4>{GROUP_TITLES[type]}</h4>
                <ul class="settings-list">
                  <For each={grouped()[type]}>
                    {(s) => (
                      <li>
                        <span>{labels()[labelKey(s)] ?? "…"}</span>
                        <span class="subscription-actions">
                          <button type="button" disabled={busyId() === s.id} onClick={() => void toggleMute(s)}>
                            {s.muted ? "Unmute" : "Mute"}
                          </button>
                          <button type="button" disabled={busyId() === s.id} onClick={() => void unsubscribe(s)}>
                            Unsubscribe
                          </button>
                        </span>
                      </li>
                    )}
                  </For>
                </ul>
              </div>
            )}
          </For>
        </Show>
      </Show>
    </section>
  );
};

export default SubscriptionsSection;
