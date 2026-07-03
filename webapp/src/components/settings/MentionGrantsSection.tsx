// /settings' mention-grants section (task C4, PLANDOC.md §7): the people
// who may always @mention-notify the caller, independent of subscriptions
// (extends "subscribed", powers "authorized" — PLANDOC.md §4/§9 decision 4).
//
// The task brief calls for "add by handle", but SettingsService.grant-mention
// takes a UserID (a ULID), not a handle — there is no handle -> id lookup op
// anywhere in CSIL (no UserService at all; see src/lib/mock/fixtures.ts's own
// note on the identical gap for post/comment authors). Rather than fake a
// resolution that doesn't exist, this form is honestly labeled "User ID".
import { createSignal, For, onMount, Show, type Component } from "solid-js";
import type { MentionGrant } from "~/gen/types.gen";
import { api } from "~/lib/api";
import { FirepitServiceError } from "~/lib/errors";

const MentionGrantsSection: Component = () => {
  const [grants, setGrants] = createSignal<MentionGrant[]>([]);
  const [loading, setLoading] = createSignal(true);
  const [error, setError] = createSignal<string | null>(null);
  const [userId, setUserId] = createSignal("");
  const [submitting, setSubmitting] = createSignal(false);
  const [revokingId, setRevokingId] = createSignal<string | null>(null);

  const describe = (err: unknown, fallback: string): string =>
    err instanceof FirepitServiceError ? err.message : fallback;

  const load = async (): Promise<void> => {
    try {
      const list = await api.settings.listMentionGrants({});
      setGrants(list.grants);
    } catch (err) {
      setError(describe(err, "Couldn't load mention grants."));
    } finally {
      setLoading(false);
    }
  };

  onMount(() => void load());

  const submit = async (e: SubmitEvent): Promise<void> => {
    e.preventDefault();
    const id = userId().trim();
    if (!id) return;
    setSubmitting(true);
    setError(null);
    const prev = grants();
    setGrants([...prev, { userId: id, createdAt: new Date() }]);
    try {
      await api.settings.grantMention(id);
      setUserId("");
    } catch (err) {
      setGrants(prev);
      setError(describe(err, "Couldn't grant mention access."));
    } finally {
      setSubmitting(false);
    }
  };

  const revoke = async (id: string): Promise<void> => {
    const prev = grants();
    setRevokingId(id);
    setError(null);
    setGrants(prev.filter((g) => g.userId !== id));
    try {
      await api.settings.revokeMention(id);
    } catch (err) {
      setGrants(prev);
      setError(describe(err, "Couldn't revoke that grant."));
    } finally {
      setRevokingId(null);
    }
  };

  return (
    <section class="settings-section">
      <h3>Mention permissions</h3>
      <p>
        People listed here can always @mention-notify you (as long as your policy above isn't "Never"). CSIL has
        no handle lookup yet, so this takes a user ID rather than a handle.
      </p>
      <Show when={error()}>
        <p class="form-error" role="alert">
          {error()}
        </p>
      </Show>
      <form class="inline-form" onSubmit={(e) => void submit(e)}>
        <label>
          User ID
          <input value={userId()} onInput={(e) => setUserId(e.currentTarget.value)} placeholder="01FPMOCKUSERBOB00000000" />
        </label>
        <button type="submit" disabled={submitting() || !userId().trim()}>
          Grant
        </button>
      </form>
      <Show when={!loading()} fallback={<p class="page-status">Loading…</p>}>
        <Show when={grants().length > 0} fallback={<p class="rail-status">No one has been granted access yet.</p>}>
          <ul class="settings-list">
            <For each={grants()}>
              {(g) => (
                <li>
                  <span class="mono">{g.userId}</span>
                  <button type="button" disabled={revokingId() === g.userId} onClick={() => void revoke(g.userId)}>
                    Revoke
                  </button>
                </li>
              )}
            </For>
          </ul>
        </Show>
      </Show>
    </section>
  );
};

export default MentionGrantsSection;
