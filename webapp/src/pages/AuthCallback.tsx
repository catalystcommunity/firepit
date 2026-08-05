// The server completes the IDP flow and creates the session before this page
// loads. Show a callback error, or refresh the session and go home.
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
