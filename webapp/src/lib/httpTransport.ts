// The browser HTTP carrier for CSIL-RPC (task C1, PLANDOC.md §3/§5): POSTs
// one CBOR envelope per call to `/csil/v1/rpc` and decodes the response,
// translating both transport-status failures and the `ServiceError`
// application-error variant into the two typed errors in `~/lib/errors` —
// this is the "one place" pages never have to parse an envelope themselves.
//
// Wire conventions this follows (api/internal/server/dispatch.go,
// api/internal/server/server.go):
//   - endpoint: same-origin `POST /csil/v1/rpc` (no `/api` prefix — unlike
//     longhouse, which mounts its RPC endpoint under `/api` so the dev-time
//     path matches the rest of that app's routes, firepit-api serves this
//     path directly; see CLAUDE.md's architecture diagram).
//   - `Content-Type: application/cbor`; the whole POST body is one request
//     envelope, the whole response body is one response envelope.
//   - the server always answers HTTP 200 for a well-formed request, even a
//     transport-status failure or an application `ServiceError` — HTTP
//     status codes are reserved for failures the envelope itself can't
//     express (wrong mount -> 404, oversized body -> 413, ...).
//   - the session lives in an httpOnly cookie (`credentials: "same-origin"`
//     is what rides it along; there is no bearer token to attach here).
import type { AsyncServiceTransport } from "~/gen/client.async.gen";
import { fromServiceErrorCbor } from "~/gen/codec.gen";
import { Status, statusName } from "~/transport/csil/conventions";
import { RpcRequest, RpcResponse } from "~/transport/csil/rpc";
import { FirepitServiceError, FirepitTransportError } from "./errors";
import { methodToOp } from "./opNaming";

export const RPC_ENDPOINT = "/csil/v1/rpc";

export interface HttpTransportOptions {
  endpoint?: string;
  /** Override for tests; defaults to the global `fetch`. */
  fetchImpl?: typeof fetch;
}

export function createHttpTransport(opts: HttpTransportOptions = {}): AsyncServiceTransport {
  const endpoint = opts.endpoint ?? RPC_ENDPOINT;
  const doFetch = opts.fetchImpl ?? fetch;

  return {
    async call(service: string, op: string, payload: Uint8Array): Promise<Uint8Array> {
      const envelope = new RpcRequest(service, methodToOp(op), payload).encode();

      let res: Response;
      try {
        res = await doFetch(endpoint, {
          method: "POST",
          credentials: "same-origin",
          headers: {
            "Content-Type": "application/cbor",
            Accept: "application/cbor",
          },
          // lib.dom's `BodyInit` wants a `Uint8Array<ArrayBuffer>` under this
          // TS/lib combination; the codec's `Uint8Array<ArrayBufferLike>`
          // return type is structurally the same bytes on every runtime that
          // matters (browser, Node, happy-dom) but doesn't satisfy the
          // stricter generic, so this is a type-only cast.
          body: envelope as unknown as BodyInit,
        });
      } catch (e) {
        throw new FirepitTransportError(
          `network error calling ${service}/${op}: ${e instanceof Error ? e.message : String(e)}`,
        );
      }

      const bodyBuf = new Uint8Array(await res.arrayBuffer());

      let resp: RpcResponse;
      try {
        resp = RpcResponse.decode(bodyBuf);
      } catch {
        throw new FirepitTransportError(
          res.ok
            ? `undecodable response envelope for ${service}/${op}`
            : `${service}/${op}: ${res.status} ${res.statusText}`,
          res.ok ? undefined : res.status,
        );
      }

      if (resp.status !== Status.Ok) {
        throw new FirepitTransportError(
          resp.error ?? `transport status ${statusName(resp.status)} calling ${service}/${op}`,
          resp.status,
        );
      }

      if (resp.variant === "ServiceError") {
        throw new FirepitServiceError(fromServiceErrorCbor(resp.payload));
      }

      return resp.payload;
    },
  };
}
