// Round-trip coverage: for a couple of firepit operations, build a sample
// request/response value, encode it with the csilgen-generated codec
// (src/gen/codec.gen.ts), wrap it in a CSIL-RPC envelope via the vendored
// transport library (src/transport/csil), and verify the envelope round-trips
// and the payload decodes back to an equal value on the other side. This is
// the TypeScript half of the seam PLANDOC.md task A2 requires — the Go test
// (api/internal/csil/rpc_roundtrip_test.go) exercises the same seam server-side.
import { describe, expect, it } from "vitest";
import { RpcRequest, RpcResponse } from "~/transport/csil/rpc";
import {
  toBeginLoginRequestCbor,
  fromBeginLoginRequestCbor,
  toBeginLoginResponseCbor,
  fromBeginLoginResponseCbor,
  toListBoardsRequestCbor,
  fromListBoardsRequestCbor,
  toBoardPageCbor,
  fromBoardPageCbor,
} from "~/gen/codec.gen";
import type { BeginLoginRequest, BeginLoginResponse, ListBoardsRequest, BoardPage } from "~/gen/types.gen";

describe("CSIL-RPC round-trip with generated codec", () => {
  it("AuthService/begin-login: request and response envelopes round-trip", () => {
    const req: BeginLoginRequest = { domain: "todandlorna.com" };
    const reqPayload = toBeginLoginRequestCbor(req);

    const envelope = new RpcRequest("AuthService", "begin-login", reqPayload);
    const encoded = envelope.encode();
    const decodedEnvelope = RpcRequest.decode(encoded);

    expect(decodedEnvelope.service).toBe("AuthService");
    expect(decodedEnvelope.op).toBe("begin-login");
    expect(decodedEnvelope.payload).toEqual(reqPayload);
    expect(fromBeginLoginRequestCbor(decodedEnvelope.payload)).toEqual(req);

    const resp: BeginLoginResponse = { redirectUrl: "https://linkkeys.todandlorna.com/login" };
    const respPayload = toBeginLoginResponseCbor(resp);
    const respEnvelope = RpcResponse.ok("BeginLoginResponse", respPayload);
    respEnvelope.id = 1;
    const respEncoded = respEnvelope.encode();
    const decodedResp = RpcResponse.decode(respEncoded);

    expect(decodedResp.id).toBe(1);
    expect(decodedResp.status).toBe(0);
    expect(decodedResp.variant).toBe("BeginLoginResponse");
    expect(decodedResp.payload).toEqual(respPayload);
    expect(fromBeginLoginResponseCbor(decodedResp.payload)).toEqual(resp);
  });

  it("BoardService/list-boards: request and response envelopes round-trip", () => {
    const req: ListBoardsRequest = { limit: 25 };
    const reqPayload = toListBoardsRequestCbor(req);

    const envelope = new RpcRequest("BoardService", "list-boards", reqPayload);
    const decodedEnvelope = RpcRequest.decode(envelope.encode());

    expect(decodedEnvelope.service).toBe("BoardService");
    expect(decodedEnvelope.op).toBe("list-boards");
    expect(fromListBoardsRequestCbor(decodedEnvelope.payload)).toEqual(req);

    const resp: BoardPage = {
      boards: [
        {
          id: "01H000000000000000000BOARD",
          slug: "firepit",
          title: "Firepit",
          kind: "discussion",
          createdBy: "01H000000000000000000USER",
          createdAt: new Date("2026-07-03T00:00:00Z"),
        },
      ],
    };
    const respPayload = toBoardPageCbor(resp);
    const respEnvelope = RpcResponse.ok("BoardPage", respPayload);
    const decodedResp = RpcResponse.decode(respEnvelope.encode());

    expect(decodedResp.variant).toBe("BoardPage");
    expect(fromBoardPageCbor(decodedResp.payload)).toEqual(resp);
  });

  it("surfaces a non-zero transport status distinctly from an application error", () => {
    const resp = RpcResponse.transportError(2, "unknown op");
    const decoded = RpcResponse.decode(resp.encode());
    expect(decoded.status).not.toBe(0);
    expect(decoded.payload.length).toBe(0);
    expect(decoded.error).toBe("unknown op");
  });
});
