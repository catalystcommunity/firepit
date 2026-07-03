// Session/auth (task C1, PLANDOC.md §3): whoami-on-boot, a `SessionProvider`
// context (user/loading/error + login/logout), consumed by `/login`,
// `/auth/callback`, and the top bar. Named `session.ts` in the task
// breakdown; it's `.tsx` here purely because `SessionProvider` is a JSX
// component and TypeScript only parses JSX in `.tsx` files — same module,
// same import path convention (`~/lib/session`).
//
// The session itself is httpOnly-cookie-based (PLANDOC.md §3: "Sessions are
// firepit's; linkkeys only verifies identity") — there is no token this
// module can read, so `whoami` on boot is the *only* source of truth for
// "who is logged in, if anyone." A `FirepitServiceError` with code
// `Unauthenticated` is the expected shape of "nobody's logged in" (see
// `api/internal/csilservices/auth.go`'s `Whoami`) and is never surfaced as
// an error to the UI; any other failure (network down, 500, ...) is kept
// distinct so the top bar can say "couldn't reach the server" instead of
// silently rendering as logged-out.
import {
  createContext,
  createSignal,
  onMount,
  useContext,
  type Accessor,
  type ParentComponent,
} from "solid-js";
import type { UserProfile } from "~/gen/types.gen";
import { api } from "./api";
import { isUnauthenticated } from "./errors";

export interface SessionContextValue {
  /** The caller's profile, or `null` while logged out (a supported, normal state — anonymous read). */
  user: Accessor<UserProfile | null>;
  /** True until the first whoami (boot, or after a login/logout) resolves. */
  loading: Accessor<boolean>;
  /** Set only for an *unexpected* whoami failure — never for "no session". */
  error: Accessor<string | null>;
  /** Begin login for a linkkeys domain: begin-login, then navigate the browser to the returned IDP URL. */
  login(domain: string): Promise<void>;
  /** Clear the session cookie server-side and reset local state. */
  logout(): Promise<void>;
  /** Re-run whoami (AuthCallback calls this after landing back from the IDP). */
  refresh(): Promise<void>;
}

const SessionContext = createContext<SessionContextValue>();

export const SessionProvider: ParentComponent = (props) => {
  const [user, setUser] = createSignal<UserProfile | null>(null);
  const [loading, setLoading] = createSignal(true);
  const [error, setError] = createSignal<string | null>(null);

  const refresh = async (): Promise<void> => {
    setLoading(true);
    try {
      const profile = await api.auth.whoami({});
      setUser(profile);
      setError(null);
    } catch (err) {
      setUser(null);
      // Anonymous is a normal, expected outcome — only a genuinely
      // unexpected failure (network/5xx/etc.) is surfaced via `error`.
      setError(isUnauthenticated(err) ? null : err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  };

  const login = async (domain: string): Promise<void> => {
    const resp = await api.auth.beginLogin({ domain });
    // A real login leaves the SPA entirely (302 to the linkkeys IDP, which
    // eventually lands back on GET /auth/callback) — this never returns in
    // practice. Guarded for non-browser test environments.
    if (typeof window !== "undefined") {
      window.location.href = resp.redirectUrl;
    }
  };

  const logout = async (): Promise<void> => {
    try {
      await api.auth.logout({});
    } finally {
      setUser(null);
      setError(null);
    }
  };

  onMount(() => {
    void refresh();
  });

  const value: SessionContextValue = { user, loading, error, login, logout, refresh };

  return <SessionContext.Provider value={value}>{props.children}</SessionContext.Provider>;
};

export function useSession(): SessionContextValue {
  const ctx = useContext(SessionContext);
  if (!ctx) throw new Error("useSession() must be called within a <SessionProvider>");
  return ctx;
}
