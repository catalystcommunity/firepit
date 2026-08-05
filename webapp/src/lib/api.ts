// Build one CSIL-RPC client for the application. Use the in-memory transport
// when `VITE_FIREPIT_MOCK` is "1" or "true".
// createApiClient also accepts an explicit override so tests do not depend
// on environment state.
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

// Share one client so that mock state does not diverge between pages.
export const api = createApiClient();
