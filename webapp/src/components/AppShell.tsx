// The app shell (task C1, PLANDOC.md §7): top bar (app name, auth state,
// placeholder bell) + left board-list rail + the routed page content, with
// the error boundary and suspense/loading conventions every page below it
// inherits for free. Passed as `@solidjs/router`'s `Router` `root` prop
// (see App.tsx) — `props.children` is whatever route matched.
import { A } from "@solidjs/router";
import { createResource, ErrorBoundary, For, Suspense, type ParentComponent } from "solid-js";
import { api } from "~/lib/api";
import { useSession } from "~/lib/session";

const AppShell: ParentComponent = (props) => {
  const session = useSession();
  const [boardPage] = createResource(() => api.board.listBoards({}));

  return (
    <div class="app-shell">
      <header class="topbar">
        <A href="/" class="brand">
          Firepit
        </A>
        <nav class="topbar-actions">
          <A href="/notifications" class="bell" aria-label="Notifications" title="Notifications">
            🔔
          </A>
          <Suspense fallback={<span class="auth-state">…</span>}>
            {session.loading() ? (
              <span class="auth-state">…</span>
            ) : session.user() ? (
              <span class="auth-state">
                {session.user()?.displayName}
                <button type="button" class="link-button" onClick={() => void session.logout()}>
                  Log out
                </button>
              </span>
            ) : (
              <A href="/login" class="auth-state">
                Log in
              </A>
            )}
          </Suspense>
        </nav>
      </header>

      <div class="app-body">
        <nav class="board-rail" aria-label="Boards">
          <h2>Boards</h2>
          <Suspense fallback={<p class="rail-status">Loading boards…</p>}>
            <ErrorBoundary fallback={<p class="rail-status">Couldn't load boards.</p>}>
              <ul>
                <For each={boardPage()?.boards ?? []}>
                  {(board) => (
                    <li>
                      <A href={`/b/${board.slug}`}>{board.title}</A>
                    </li>
                  )}
                </For>
              </ul>
              {boardPage()?.boards.length === 0 && <p class="rail-status">No boards yet.</p>}
            </ErrorBoundary>
          </Suspense>
        </nav>

        <main class="page">
          <ErrorBoundary fallback={(err) => <p class="page-error">Something went wrong: {String(err)}</p>}>
            <Suspense fallback={<p class="page-status">Loading…</p>}>{props.children}</Suspense>
          </ErrorBoundary>
        </main>
      </div>
    </div>
  );
};

export default AppShell;
