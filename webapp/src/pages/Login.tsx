// /login (task C1, PLANDOC.md §3/§5): linkkeys domain entry -> begin-login
// -> redirect to the URL the server returns. All the actual auth logic
// lives in `useSession().login` (src/lib/session.tsx); this page is just
// the form + its own request state (submitting/error).
import { createSignal, type Component } from "solid-js";
import { useSession } from "~/lib/session";

const Login: Component = () => {
  const session = useSession();
  const [domain, setDomain] = createSignal("");
  const [submitting, setSubmitting] = createSignal(false);
  const [error, setError] = createSignal<string | null>(null);

  const onSubmit = async (e: SubmitEvent): Promise<void> => {
    e.preventDefault();
    const d = domain().trim();
    if (d.length === 0) {
      setError("Enter a linkkeys domain (the part after the @ in user@domain).");
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      await session.login(d);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      setSubmitting(false);
    }
  };

  return (
    <section class="login-page">
      <h2>Log in</h2>
      <p>Enter your linkkeys domain — the part of your identity after the @ in user@domain.</p>
      <form onSubmit={(e) => void onSubmit(e)}>
        <label>
          Domain
          <input
            type="text"
            placeholder="todandlorna.com"
            value={domain()}
            disabled={submitting()}
            onInput={(e) => setDomain(e.currentTarget.value)}
          />
        </label>
        <button type="submit" disabled={submitting()}>
          {submitting() ? "Redirecting…" : "Continue"}
        </button>
      </form>
      {error() && <p class="form-error">{error()}</p>}
    </section>
  );
};

export default Login;
