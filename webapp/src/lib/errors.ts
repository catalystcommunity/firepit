// Typed errors shared by the HTTP and in-memory CSIL transports.
import type { ServiceError } from "~/gen/types.gen";

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

/**
 * A typed application-level failure: the op has a declared `/ ServiceError`
 * arm and the server (or the mock) returned one. `code` is stable to branch
 * on (see `ServiceErrorCode`); `message` is a diagnostic, not guaranteed
 * stable enough to pattern-match.
 */
export class FirepitServiceError extends Error {
  readonly code: number;
  readonly field?: string;
  readonly resourceType?: string;

  constructor(se: ServiceError) {
    super(se.message);
    this.name = "FirepitServiceError";
    this.code = se.code;
    this.field = se.field;
    this.resourceType = se.resourceType;
  }
}

/** True when `err` is the specific, expected "no session" outcome of whoami. */
export function isUnauthenticated(err: unknown): boolean {
  return err instanceof FirepitServiceError && err.code === ServiceErrorCode.Unauthenticated;
}

/**
 * A transport-level failure: the carrier itself failed — a network error, an
 * undecodable/non-2xx HTTP response, or a non-zero CSIL-RPC transport status
 * (see `~/transport/csil/conventions`'s `Status` registry). Distinct from
 * `FirepitServiceError` so callers can tell "the server understood the
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
