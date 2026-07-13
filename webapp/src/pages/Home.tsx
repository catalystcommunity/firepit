// The "/" route (task C2, PLANDOC.md §7): the board index — every board,
// grouped announce vs. discussion, with subscribe toggles and unread dots.
import { Suspense, type Component } from "solid-js";
import BoardIndexList from "~/components/board-list/BoardIndexList";
import { useSession } from "~/lib/session";
import { startUnreadPoller } from "~/lib/unread";

const Home: Component = () => {
  const session = useSession();
  const poller = startUnreadPoller(() => session.user() !== null);

  return (
    <section class="home-page">
      <div class="home-hero">
        <p class="eyebrow">Project coordination forum</p>
        <h1>{session.user() ? `Welcome back, ${session.user()?.displayName}.` : "Welcome to Firepit."}</h1>
        <p>
          Follow the boards you care about, keep GitHub activity in the same conversation, and use named
          endorsements when you want to stand behind a reply.
        </p>
        <div class="home-principles" aria-label="Firepit principles">
          <span>Threaded by context</span>
          <span>No ranking games</span>
          <span>Subscriptions first</span>
        </div>
      </div>
      {/* Its own Suspense boundary, separate from AppShell's page-level one —
          the board index's own list-boards/list-subscriptions resources
          shouldn't hide the greeting above while they load. */}
      <Suspense fallback={<p class="rail-status">Loading boards…</p>}>
        <BoardIndexList poller={poller} />
      </Suspense>
    </section>
  );
};

export default Home;
