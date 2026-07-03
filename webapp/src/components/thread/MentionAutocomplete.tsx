// A simple `@handle` picker fed by handles already present in the thread
// (task C3 scope item 2). FUTURE WORK, stated plainly: CSIL has no
// user-search op in v1 (PLANDOC.md §5 lists no such op on any service) — a
// real mention picker needs one (e.g. a paginated `UserService.search-users`
// or similar) so it can suggest *any* linkkeys identity, not just people who
// happen to have been `@mentioned` or to have posted in this thread already.
// Until that lands, this component's candidate list is derived client-side
// from `extractMentionHandles` over the thread's own markdown (see
// `~/lib/markdown.ts`) — good enough to demonstrate the interaction, not a
// claim about completeness.
import { For, Show, type Component } from "solid-js";

export interface MentionAutocompleteProps {
  candidates: string[];
  query: string;
  onPick: (handle: string) => void;
}

const MentionAutocomplete: Component<MentionAutocompleteProps> = (props) => {
  const matches = () =>
    props.candidates.filter((h) => h.toLowerCase().startsWith(props.query.toLowerCase())).slice(0, 6);

  return (
    <Show when={matches().length > 0}>
      <ul class="mention-list" role="listbox" aria-label="Mention suggestions">
        <For each={matches()}>
          {(handle) => (
            <li>
              <button type="button" class="mention-option" onClick={() => props.onPick(handle)}>
                @{handle}
              </button>
            </li>
          )}
        </For>
      </ul>
    </Show>
  );
};

export default MentionAutocomplete;
