// The one place the app builds a CSIL-RPC client (task C1, PLANDOC.md §7).
// Pages/components import the `api` singleton below; nothing outside this
// module (and the transports it wires up) touches a `ServiceTransport`
// directly.
//
// Mode selection: real HTTP transport by default, or the in-memory mock
// (src/lib/mock) when `VITE_FIREPIT_MOCK` is "1"/"true" — the switch C2-C4
// need so they can build against realistic data without waiting on the Go
// API. `npm run dev` picks this up from a `.env`/shell var; `createApiClient`
// also takes an explicit override so tests never depend on env state.
import { AsyncApiClient } from "~/gen/client.async.gen";
import { createHttpTransport } from "./httpTransport";
import { createMockTransport, type MockTransport } from "./mock/mockTransport";

export interface CreateApiClientOptions {
  /** Force mock mode regardless of `VITE_FIREPIT_MOCK` (tests use this). */
  mock?: boolean;
  /** Share/inspect the mock's fixture state (ignored unless mock mode is active). */
  mockTransport?: MockTransport;
}

export function isMockModeEnabled(): boolean {
  const flag = import.meta.env.VITE_FIREPIT_MOCK;
  return flag === "1" || flag === "true";
}

export function createApiClient(opts: CreateApiClientOptions = {}): AsyncApiClient {
  const useMock = opts.mock ?? isMockModeEnabled();
  const transport = useMock ? (opts.mockTransport ?? createMockTransport()) : createHttpTransport();
  return new AsyncApiClient(transport);
}

// The module-singleton client every page/store shares for the app's
// lifetime — one real transport (one `fetch` target), or one mock fixture
// store, never several independent ones drifting out of sync.
export const api = createApiClient();
