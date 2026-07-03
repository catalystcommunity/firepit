// Shared wire-naming helper between every ServiceTransport this app builds
// (the real HTTP carrier and the in-memory mock carrier both need it).
//
// csilgen's generated clients (src/gen/client.async.gen.ts) hand a
// PascalCase method name straight off the CSIL schema to the transport seam
// (e.g. "BeginLogin", "ListBoards" — see AuthAsyncClient.beginLogin's
// `this.t.call("auth", "BeginLogin", ...)`). Every real CSIL host in this
// org kebab-cases that before it hits the wire — this is longhouse's
// `methodToOp` (webapp/src/transport/csilrpc.ts there), and firepit-api's
// own dispatch table (api/internal/server/dispatch.go) is keyed directly on
// the resulting kebab-case op names ("begin-login", "list-boards", ...) with
// no server-side conversion. `service` needs no conversion — the generated
// client already lowercases it ("auth", "board", ...).
export function methodToOp(method: string): string {
  return method.replace(/([a-z0-9])([A-Z])/g, "$1-$2").toLowerCase();
}
