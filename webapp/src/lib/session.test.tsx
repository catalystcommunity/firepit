// SessionProvider behavior against a fully-controlled fake transport (task
// C1 accept criterion: "session provider behavior with the mock carrier —
// logged-out -> login flow state change"). `~/lib/api`'s singleton is
// mocked outright (rather than driving the real mock transport) so every
// scenario — including the two whoami can't produce from the seeded store
// alone (an unauthenticated start, and a genuinely unexpected failure) — is
// deterministic and independent of fixture content.
import { render, waitFor } from "@solidjs/testing-library";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { UserProfile } from "~/gen/types.gen";
import { FirepitServiceError, ServiceErrorCode } from "./errors";

const { whoami, beginLogin, logout } = vi.hoisted(() => ({
  whoami: vi.fn(),
  beginLogin: vi.fn(),
  logout: vi.fn(),
}));

vi.mock("./api", () => ({
  api: { auth: { whoami, beginLogin, logout } },
}));

// Imported after the mock so session.tsx picks up the faked `api`.
const { SessionProvider, useSession } = await import("./session");

const PROFILE: UserProfile = {
  id: "01FPTESTUSER",
  linkkeysDomain: "example.com",
  handle: "alice",
  displayName: "Alice Example",
  kind: "human",
  roles: [],
  createdAt: new Date("2026-01-01T00:00:00Z"),
};

const unauthenticated = () =>
  Promise.reject(new FirepitServiceError({ code: ServiceErrorCode.Unauthenticated, message: "no active session" }));

// Renders nothing visible; hands the live session object out via `onReady`
// so tests can drive login()/logout()/refresh() and assert on the signals
// directly, the same shape a real page's `useSession()` call has.
function Harness(props: { onReady: (session: ReturnType<typeof useSession>) => void }) {
  const session = useSession();
  // A Solid component body runs exactly once — this is that one intentional
  // synchronous capture (test-only escape hatch to hand the live session
  // object to the surrounding `it()`), not a stale-closure bug.
  // eslint-disable-next-line solid/reactivity
  props.onReady(session);
  return null;
}

function renderSession() {
  let session!: ReturnType<typeof useSession>;
  render(() => (
    <SessionProvider>
      <Harness onReady={(s) => (session = s)} />
    </SessionProvider>
  ));
  return () => session;
}

beforeEach(() => {
  whoami.mockReset();
  beginLogin.mockReset();
  logout.mockReset();
});

describe("SessionProvider", () => {
  it("boots logged-out when whoami reports Unauthenticated — not as an error", async () => {
    whoami.mockImplementation(unauthenticated);
    const getSession = renderSession();

    await waitFor(() => expect(getSession().loading()).toBe(false));
    expect(getSession().user()).toBeNull();
    expect(getSession().error()).toBeNull();
  });

  it("login() begins login and navigates the browser to the returned redirect URL", async () => {
    whoami.mockImplementation(unauthenticated);
    beginLogin.mockResolvedValue({ redirectUrl: "/auth/callback?mock_domain=example.com" });
    const getSession = renderSession();
    await waitFor(() => expect(getSession().loading()).toBe(false));

    await getSession().login("example.com");

    expect(beginLogin).toHaveBeenCalledWith({ domain: "example.com" });
    expect(window.location.href).toContain("/auth/callback?mock_domain=example.com");
  });

  it("refresh() after a successful whoami transitions logged-out -> logged-in", async () => {
    whoami.mockImplementationOnce(unauthenticated);
    const getSession = renderSession();
    await waitFor(() => expect(getSession().loading()).toBe(false));
    expect(getSession().user()).toBeNull();

    // What AuthCallback does once the server has minted the session cookie.
    whoami.mockResolvedValueOnce(PROFILE);
    await getSession().refresh();

    expect(getSession().user()).toEqual(PROFILE);
    expect(getSession().loading()).toBe(false);
  });

  it("logout() clears the user even if the server call fails", async () => {
    whoami.mockResolvedValueOnce(PROFILE);
    const getSession = renderSession();
    await waitFor(() => expect(getSession().user()).toEqual(PROFILE));

    logout.mockRejectedValueOnce(new Error("network blip"));
    await expect(getSession().logout()).rejects.toThrow("network blip");
    expect(getSession().user()).toBeNull();
  });

  it("surfaces a genuinely unexpected whoami failure via error(), distinct from logged-out", async () => {
    whoami.mockRejectedValueOnce(new Error("the server is on fire"));
    const getSession = renderSession();

    await waitFor(() => expect(getSession().loading()).toBe(false));
    expect(getSession().user()).toBeNull();
    expect(getSession().error()).toBe("the server is on fire");
  });
});
