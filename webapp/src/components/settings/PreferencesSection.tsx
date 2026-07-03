// /settings' mention-policy + notify-on-endorse section (task C4,
// PLANDOC.md §7, §9 decision 4). Every mutation is optimistic (the radio/
// toggle flips immediately) with rollback to the prior value and an inline
// ServiceError message on failure — no full-page reload, no silent
// swallow.
import { createSignal, For, onMount, Show, type Component } from "solid-js";
import type { MentionPolicy, UserSettings } from "~/gen/types.gen";
import { api } from "~/lib/api";
import { FirepitServiceError } from "~/lib/errors";

interface PolicyOption {
  value: MentionPolicy;
  label: string;
  description: string;
}

// Human explanations per PLANDOC.md §9 decision 4 ("Mentions — ... Default
// policy `subscribed` ...; `mention_grants` extends that per-person;
// `everyone`/`authorized`/`nobody` policies available").
const POLICIES: PolicyOption[] = [
  {
    value: "subscribed",
    label: "Only in places I'm already subscribed to",
    description:
      "The default. A mention notifies you when it happens inside a board, post, or comment thread you " +
      "already subscribe to — or from anyone listed under \"Mention permissions\" below, regardless of " +
      "subscriptions.",
  },
  {
    value: "everyone",
    label: "Anyone, anywhere",
    description: "Any @mention notifies you, from anyone, in any board — subscriptions don't matter.",
  },
  {
    value: "authorized",
    label: "Only people I've granted",
    description:
      "Mentions notify you only from the people listed under \"Mention permissions\" below. Subscriptions " +
      "don't extend it the way they do under \"subscribed\".",
  },
  {
    value: "nobody",
    label: "Never",
    description: "Mentions never notify you. The @handle still renders as plain text for anyone reading the thread.",
  },
];

const PreferencesSection: Component = () => {
  const [settings, setSettings] = createSignal<UserSettings | null>(null);
  const [loading, setLoading] = createSignal(true);
  const [error, setError] = createSignal<string | null>(null);
  const [savingPolicy, setSavingPolicy] = createSignal(false);
  const [savingEndorse, setSavingEndorse] = createSignal(false);

  const describe = (err: unknown, fallback: string): string =>
    err instanceof FirepitServiceError ? err.message : fallback;

  onMount(async () => {
    try {
      setSettings(await api.settings.getSettings({}));
    } catch (err) {
      setError(describe(err, "Couldn't load settings."));
    } finally {
      setLoading(false);
    }
  });

  const setPolicy = async (value: MentionPolicy): Promise<void> => {
    const prev = settings();
    if (!prev || prev.mentionPolicy === value) return;
    setError(null);
    setSettings({ ...prev, mentionPolicy: value });
    setSavingPolicy(true);
    try {
      setSettings(await api.settings.updateSettings({ mentionPolicy: value }));
    } catch (err) {
      setSettings(prev);
      setError(describe(err, "Couldn't update mention policy."));
    } finally {
      setSavingPolicy(false);
    }
  };

  const toggleEndorse = async (): Promise<void> => {
    const prev = settings();
    if (!prev) return;
    const next = !prev.notifyOnEndorse;
    setError(null);
    setSettings({ ...prev, notifyOnEndorse: next });
    setSavingEndorse(true);
    try {
      setSettings(await api.settings.updateSettings({ notifyOnEndorse: next }));
    } catch (err) {
      setSettings(prev);
      setError(describe(err, "Couldn't update that setting."));
    } finally {
      setSavingEndorse(false);
    }
  };

  return (
    <section class="settings-section">
      <h3>Mentions &amp; endorsements</h3>
      <Show when={error()}>
        <p class="form-error" role="alert">
          {error()}
        </p>
      </Show>
      <Show when={!loading()} fallback={<p class="page-status">Loading…</p>}>
        <Show when={settings()}>
          {(s) => (
            <>
              <fieldset class="mention-policy" disabled={savingPolicy()}>
                <legend>Who can @mention-notify me</legend>
                <For each={POLICIES}>
                  {(policy) => (
                    <label class="policy-option">
                      <input
                        type="radio"
                        name="mention-policy"
                        value={policy.value}
                        checked={s().mentionPolicy === policy.value}
                        onChange={() => void setPolicy(policy.value)}
                      />
                      <span>
                        <strong>{policy.label}</strong>
                        <p>{policy.description}</p>
                      </span>
                    </label>
                  )}
                </For>
              </fieldset>

              <label class="settings-toggle">
                <input
                  type="checkbox"
                  checked={s().notifyOnEndorse}
                  disabled={savingEndorse()}
                  onChange={() => void toggleEndorse()}
                />
                Notify me when someone endorses my posts or comments
              </label>
            </>
          )}
        </Show>
      </Show>
    </section>
  );
};

export default PreferencesSection;
