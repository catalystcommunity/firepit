// /settings' mention-grants section (task C4, PLANDOC.md §7): the people
// who may always @mention-notify the caller, independent of subscriptions
// (extends "subscribed", powers "authorized" — PLANDOC.md §4/§9 decision 4).
//
// SettingsService.grant-mention/revoke-mention still take a UserID (a ULID),
// but the form itself now accepts a handle: SocialService.resolve-user (a
// CSIL schema follow-up) turns the typed handle into the UserID before
// calling grant-mention, so the caller never has to paste a raw id.
import { createSignal, For, onMount, Show, type Component } from "solid-js";
import type { MentionGrant } from "~/gen/types.gen";
import { api } from "~/lib/api";
import { FirepitServiceError, ServiceErrorCode } from "~/lib/errors";

const MentionGrantsSection: Component = () => {
  const [grants, setGrants] = createSignal<MentionGrant[]>([]);
  const [loading, setLoading] = createSignal(true);
  const [error, setError] = createSignal<string | null>(null);
  const [handle, setHandle] = createSignal("");
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
    const typed = handle().trim().replace(/^@/, "");
    if (!typed) return;
    setSubmitting(true);
    setError(null);
    try {
      const profile = await api.social.resolveUser(typed);
      const prev = grants();
      setGrants([...prev, { userId: profile.id, createdAt: new Date() }]);
      try {
        await api.settings.grantMention(profile.id);
        setHandle("");
      } catch (err) {
        setGrants(prev);
        setError(describe(err, "Couldn't grant mention access."));
      }
    } catch (err) {
      setError(
        err instanceof FirepitServiceError && err.code === ServiceErrorCode.NotFound
          ? `No user found with handle "${typed}".`
          : describe(err, "Couldn't look up that handle."),
      );
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
      <p>People listed here can always @mention-notify you (as long as your policy above isn't "Never").</p>
      <Show when={error()}>
        <p class="form-error" role="alert">
          {error()}
        </p>
      </Show>
      <form class="inline-form" onSubmit={(e) => void submit(e)}>
        <label>
          Handle
          <input value={handle()} onInput={(e) => setHandle(e.currentTarget.value)} placeholder="bob" />
        </label>
        <button type="submit" disabled={submitting() || !handle().trim()}>
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
