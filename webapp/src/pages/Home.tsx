// The "/" route (task C1). C2 replaces this with the real board index; for
// now it's a minimal landing page confirming the shell + session wiring
// work end to end (auth state comes from the top bar, board links from the
// rail — this page itself only needs to greet).
import type { Component } from "solid-js";
import { useSession } from "~/lib/session";

const Home: Component = () => {
  const session = useSession();

  return (
    <section>
      <h2>{session.user() ? `Welcome back, ${session.user()?.displayName}.` : "Welcome to Firepit."}</h2>
      <p>A dev coordination forum for open source projects — threaded discussion, endorsements, no ranking.</p>
      <p>Pick a board from the left to start reading (board index and post lists land in task C2).</p>
    </section>
  );
};

export default Home;
