// /settings' friend-groups section (task C4, PLANDOC.md §4/§7): private
// groupings the caller owns, used to order endorser names (friends first,
// see PLANDOC.md §4's endorser-ordering design) — create/delete groups,
// add/remove members. Same "no handle lookup" honesty note as
// MentionGrantsSection.tsx: membership is by user ID.
import { createSignal, For, onMount, Show, type Component } from "solid-js";
import type { FriendGroup } from "~/gen/types.gen";
import { api } from "~/lib/api";
import { FirepitServiceError } from "~/lib/errors";

const FriendGroupsSection: Component = () => {
  const [groups, setGroups] = createSignal<FriendGroup[]>([]);
  const [loading, setLoading] = createSignal(true);
  const [error, setError] = createSignal<string | null>(null);
  const [newName, setNewName] = createSignal("");
  const [creating, setCreating] = createSignal(false);
  const [memberInputs, setMemberInputs] = createSignal<Record<string, string>>({});
  const [busyGroupId, setBusyGroupId] = createSignal<string | null>(null);

  const describe = (err: unknown, fallback: string): string =>
    err instanceof FirepitServiceError ? err.message : fallback;

  const load = async (): Promise<void> => {
    try {
      const list = await api.social.listFriendGroups({});
      setGroups(list.groups);
    } catch (err) {
      setError(describe(err, "Couldn't load friend groups."));
    } finally {
      setLoading(false);
    }
  };

  onMount(() => void load());

  const createGroup = async (e: SubmitEvent): Promise<void> => {
    e.preventDefault();
    const name = newName().trim();
    if (!name) return;
    setCreating(true);
    setError(null);
    try {
      const group = await api.social.createFriendGroup({ name });
      setGroups((prev) => [...prev, group]);
      setNewName("");
    } catch (err) {
      setError(describe(err, "Couldn't create that group."));
    } finally {
      setCreating(false);
    }
  };

  const deleteGroup = async (id: string): Promise<void> => {
    const prev = groups();
    setBusyGroupId(id);
    setError(null);
    setGroups(prev.filter((g) => g.id !== id));
    try {
      await api.social.deleteFriendGroup(id);
    } catch (err) {
      setGroups(prev);
      setError(describe(err, "Couldn't delete that group."));
    } finally {
      setBusyGroupId(null);
    }
  };

  const addMember = async (group: FriendGroup, e: SubmitEvent): Promise<void> => {
    e.preventDefault();
    const userId = (memberInputs()[group.id] ?? "").trim();
    if (!userId) return;
    const prev = groups();
    setError(null);
    setGroups((cur) => cur.map((g) => (g.id === group.id ? { ...g, members: [...g.members, userId] } : g)));
    try {
      await api.social.addFriend({ groupId: group.id, userId });
      setMemberInputs((cur) => ({ ...cur, [group.id]: "" }));
    } catch (err) {
      setGroups(prev);
      setError(describe(err, "Couldn't add that member."));
    }
  };

  const removeMember = async (group: FriendGroup, userId: string): Promise<void> => {
    const prev = groups();
    setError(null);
    setGroups((cur) => cur.map((g) => (g.id === group.id ? { ...g, members: g.members.filter((m) => m !== userId) } : g)));
    try {
      await api.social.removeFriend({ groupId: group.id, userId });
    } catch (err) {
      setGroups(prev);
      setError(describe(err, "Couldn't remove that member."));
    }
  };

  return (
    <section class="settings-section">
      <h3>Friend groups</h3>
      <p>
        Private to you — used to order endorser names (friends first) on posts and comments. Membership is by
        user ID for the same reason as mention grants above: no handle lookup yet.
      </p>
      <Show when={error()}>
        <p class="form-error" role="alert">
          {error()}
        </p>
      </Show>
      <form class="inline-form" onSubmit={(e) => void createGroup(e)}>
        <label>
          New group name
          <input value={newName()} onInput={(e) => setNewName(e.currentTarget.value)} placeholder="Core reviewers" />
        </label>
        <button type="submit" disabled={creating() || !newName().trim()}>
          Create group
        </button>
      </form>

      <Show when={!loading()} fallback={<p class="page-status">Loading…</p>}>
        <Show when={groups().length > 0} fallback={<p class="rail-status">No friend groups yet.</p>}>
          <ul class="friend-groups">
            <For each={groups()}>
              {(group) => (
                <li class="friend-group">
                  <div class="friend-group-header">
                    <h4>{group.name}</h4>
                    <button type="button" disabled={busyGroupId() === group.id} onClick={() => void deleteGroup(group.id)}>
                      Delete group
                    </button>
                  </div>
                  <ul class="settings-list">
                    <For each={group.members} fallback={<li class="rail-status">No members yet.</li>}>
                      {(memberId) => (
                        <li>
                          <span class="mono">{memberId}</span>
                          <button type="button" onClick={() => void removeMember(group, memberId)}>
                            Remove
                          </button>
                        </li>
                      )}
                    </For>
                  </ul>
                  <form class="inline-form" onSubmit={(e) => void addMember(group, e)}>
                    <label>
                      Add member (user ID)
                      <input
                        value={memberInputs()[group.id] ?? ""}
                        onInput={(e) => {
                          const value = e.currentTarget.value;
                          setMemberInputs((cur) => ({ ...cur, [group.id]: value }));
                        }}
                      />
                    </label>
                    <button type="submit">Add</button>
                  </form>
                </li>
              )}
            </For>
          </ul>
        </Show>
      </Show>
    </section>
  );
};

export default FriendGroupsSection;
