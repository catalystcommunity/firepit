// Typed errors shared by the HTTP and in-memory CSIL transports.
import { ServiceErrorTypeRefusal as ServiceErrorRefusal } from "~/gen/codec.gen";

export { ServiceErrorRefusal };

// Mirrors api/internal/csilservices/errors.go's fixed code enumeration —
// keep the two in sync (that file is the source of truth; see its own
// comment pointing at docs/OPERATING.md's error code table once it exists).
export const ServiceErrorCode = {
  Validation: 2,
  Unauthenticated: 3,
  Forbidden: 4,
  NotFound: 5,
  Conflict: 6,
  Internal: 7,
} as const;

/** Return the diagnostic from a declared service error or a local failure. */
export function errorMessage(err: unknown): string {
  if (err instanceof ServiceErrorRefusal) return err.value.message;
  return err instanceof Error ? err.message : String(err);
}

/** True when `err` is the specific, expected "no session" outcome of whoami. */
export function isUnauthenticated(err: unknown): boolean {
  return err instanceof ServiceErrorRefusal && err.value.code === ServiceErrorCode.Unauthenticated;
}

/**
 * A transport-level failure: the carrier itself failed — a network error, an
 * undecodable/non-2xx HTTP response, or a non-zero CSIL-RPC transport status
 * (see `~/transport/csil/conventions`'s `Status` registry). Distinct from
 * `ServiceErrorRefusal` so callers can tell "the server understood the
 * request and rejected it" apart from "the request never got a typed answer
 * at all". `status`, when present, is the transport status code (not an
 * HTTP status).
 */
export class FirepitTransportError extends Error {
  readonly status?: number;

  constructor(message: string, status?: number) {
    super(message);
    this.name = "FirepitTransportError";
    this.status = status;
  }
}
