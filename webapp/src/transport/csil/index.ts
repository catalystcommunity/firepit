/**
 * VENDORED copy of the canonical CSIL-RPC reference TypeScript transport
 * (`csilgen-transport`, from csilgen/transports/typescript).
 *
 * Copied here verbatim (cbor.ts, conventions.ts, carrier.ts, rpc.ts) rather
 * than depending on the unpublished npm package, so the build stays
 * self-contained while upstream stabilizes — the same call linkkeys made
 * when it vendored the Rust sibling, and the same approach longhouse uses
 * for its own webapp/src/transport/csil. Unlike longhouse's copy, the `.ts`
 * import extensions between these files are kept as-is (not stripped): this
 * project's tsconfig.app.json sets `allowImportingTsExtensions: true` under
 * `moduleResolution: "bundler"`, so extension-ful relative imports resolve
 * fine and the files stay byte-identical to upstream. The codec is
 * byte-compatible with the Go and Rust references (verified by the shared
 * conformance vectors in conformance.rpc.json).
 *
 * Do not hand-edit the copied files; re-copy from upstream so the wire stays
 * in lockstep with the server.
 */
export { RpcRequest, RpcResponse, RpcPush } from "./rpc.ts";
export { Status, TransportError, statusName } from "./conventions.ts";
export { encode as encodeCbor, decode as decodeCbor } from "./cbor.ts";
export type { FrameCarrier } from "./carrier.ts";
