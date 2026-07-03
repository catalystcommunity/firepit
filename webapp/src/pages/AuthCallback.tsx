// /auth/callback (task C1, PLANDOC.md §3): the SPA-side landing page for the
// login flow. The IDP round-trip itself (decrypt-token -> verify-assertion
// -> userinfo-fetch -> mint session cookie) is entirely server-side
// (GET /auth/callback on firepit-api, api/internal/server/authcallback.go) —
// by the time the browser is here the cookie is already set (or the flow
// failed and the server redirected with an `error` query param instead).
// This page's only job is: if `error` is present, show it; otherwise re-run
// whoami (the only source of truth for "who's logged in") and go home.
import { A, useNavigate, useSearchParams } from "@solidjs/router";
import { createSignal, onMount, type Component } from "solid-js";
import { useSession } from "~/lib/session";

const AuthCallback: Component = () => {
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const session = useSession();
  const [error, setError] = createSignal<string | null>(null);

  onMount(() => {
    const fromQuery = params.error;
    if (typeof fromQuery === "string" && fromQuery.length > 0) {
      setError(fromQuery);
      return;
    }
    void (async () => {
      await session.refresh();
      if (session.user()) {
        navigate("/", { replace: true });
      } else {
        setError(session.error() ?? "Login didn't complete — no session was established.");
      }
    })();
  });

  return (
    <section class="auth-callback">
      {error() ? (
        <>
          <h2>Login failed</h2>
          <p>{error()}</p>
          <A href="/login">Try again</A>
        </>
      ) : (
        <p>Finishing login…</p>
      )}
    </section>
  );
};

export default AuthCallback;
