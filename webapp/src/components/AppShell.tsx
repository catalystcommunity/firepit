// The router uses AppShell as its shared page frame. On narrow screens, the
// board rail becomes a drawer. Navigation and Escape close that drawer.
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
  // Use a separate poller so that page-level polling cannot stop rail updates.
  const poller = startUnreadPoller(() => session.user() !== null);

  const [drawerOpen, setDrawerOpen] = createSignal(false);
  const closeDrawer = (): void => {
    setDrawerOpen(false);
  };

  // Do not keep the mobile drawer open after navigation.
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
          <Show when={session.user()?.roles.includes("admin")}>
            <A href="/admin/categories" class="auth-state">Categories</A>
          </Show>
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
