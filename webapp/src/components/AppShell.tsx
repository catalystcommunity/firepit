// The app shell (task C1, PLANDOC.md §7): top bar (app name, auth state,
// placeholder bell) + left board-list rail + the routed page content, with
// the error boundary and suspense/loading conventions every page below it
// inherits for free. Passed as `@solidjs/router`'s `Router` `root` prop
// (see App.tsx) — `props.children` is whatever route matched.
//
// Mobile nav (docs/DESIGN.md "mobile patterns"): below ~760px the board
// rail becomes an off-canvas drawer instead of a permanent column — a menu
// button in the topbar (`.rail-toggle`, hidden above that breakpoint via
// CSS) toggles it, a backdrop click / Escape / picking a board all close
// it. Above ~760px `drawerOpen` is simply never turned on (no toggle button
// visible to set it), so the rail renders exactly as it always has —
// `.board-rail`'s own CSS is what actually switches between "static column"
// and "fixed slide-in panel" per breakpoint.
import { A, useLocation } from "@solidjs/router";
import {
  createEffect,
  createResource,
  createSignal,
  ErrorBoundary,
  For,
  onCleanup,
  onMount,
  Show,
  Suspense,
  type ParentComponent,
} from "solid-js";
import "~/components/board-list/board-list.css";
import FlameMark from "~/components/FlameMark";
import NotificationBell from "~/components/notifications/NotificationBell";
import ThemeToggle from "~/components/ThemeToggle";
import { api } from "~/lib/api";
import { useSession } from "~/lib/session";
import { boardUnreadCount, startUnreadPoller } from "~/lib/unread";

const AppShell: ParentComponent = (props) => {
  const session = useSession();
  const location = useLocation();
  const [boardPage] = createResource(() => api.board.listBoards({}));
  // Task C2's shared unread poller (~/lib/unread) — its own instance here so
  // the rail's dots update independently of any page-level instance (see
  // that module's "safe to call more than once" doc comment); C4's bell is
  // expected to do the same rather than reach into this one.
  const poller = startUnreadPoller(() => session.user() !== null);

  const [drawerOpen, setDrawerOpen] = createSignal(false);
  const closeDrawer = (): void => {
    setDrawerOpen(false);
  };

  // Never leave the drawer open across a navigation (picking a board on
  // mobile should close it behind you) or stuck open if the viewport grows
  // past the mobile breakpoint mid-session.
  createEffect(() => {
    void location.pathname;
    closeDrawer();
  });

  const handleKey = (e: KeyboardEvent): void => {
    if (e.key === "Escape") closeDrawer();
  };
  onMount(() => document.addEventListener("keydown", handleKey));
  onCleanup(() => document.removeEventListener("keydown", handleKey));

  return (
    <div class="app-shell">
      <header class="topbar">
        <div class="topbar-start">
          <button
            type="button"
            class="rail-toggle"
            aria-expanded={drawerOpen()}
            aria-controls="board-rail"
            aria-label="Toggle boards menu"
            onClick={() => setDrawerOpen((v) => !v)}
          >
            <span aria-hidden="true">☰</span>
          </button>
          <A href="/" class="brand">
            <FlameMark />
            Firepit
          </A>
        </div>
        <nav class="topbar-actions">
          <ThemeToggle />
          <NotificationBell />
          <Suspense fallback={<span class="auth-state">…</span>}>
            {session.loading() ? (
              <span class="auth-state">…</span>
            ) : session.user() ? (
              <span class="auth-state">
                <span class="auth-name">{session.user()?.displayName}</span>
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
        <Show when={drawerOpen()}>
          <div class="rail-backdrop" onClick={closeDrawer} />
        </Show>
        <nav id="board-rail" class="board-rail" classList={{ "is-open": drawerOpen() }} aria-label="Boards">
          <div class="board-rail-header">
            <h2>Boards</h2>
            <button type="button" class="rail-close" aria-label="Close boards menu" onClick={closeDrawer}>
              <span aria-hidden="true">×</span>
            </button>
          </div>
          <Suspense fallback={<p class="rail-status">Loading boards…</p>}>
            <ErrorBoundary fallback={<p class="rail-status">Couldn't load boards.</p>}>
              <ul>
                <For each={boardPage()?.boards ?? []}>
                  {(board) => (
                    <li>
                      <A href={`/b/${board.slug}`}>
                        <Show when={boardUnreadCount(poller.summary(), board.id) > 0}>
                          <span class="unread-dot" aria-label="Unread activity" />
                        </Show>
                        {board.title}
                      </A>
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
