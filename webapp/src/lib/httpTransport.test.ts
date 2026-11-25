// Tests for the browser HTTP carrier (task C1): the request it sends, and
// the three ways a call can conclude — a typed success payload, a
// ServiceErrorRefusal (the ServiceError variant), or a FirepitTransportError
// (network failure / undecodable response / non-zero transport status).
import { describe, expect, it, vi } from "vitest";
import { AuthAsyncClient, BoardAsyncClient } from "~/gen/client.async.gen";
import { fromBeginLoginRequestCbor, toBeginLoginResponseCbor, toServiceErrorTypeCbor } from "~/gen/codec.gen";
import { Status } from "~/transport/csil/conventions";
import { RpcRequest, RpcResponse } from "~/transport/csil/rpc";
import { ServiceErrorRefusal, FirepitTransportError } from "./errors";
import { createHttpTransport, RPC_ENDPOINT } from "./httpTransport";

function fetchReturning(response: Response): typeof fetch {
  return vi.fn().mockResolvedValue(response) as unknown as typeof fetch;
}

// Same lib.dom `BodyInit` generic nitpick as httpTransport.ts's request body
// cast (Uint8Array<ArrayBufferLike> vs. the stricter Uint8Array<ArrayBuffer>
// this TS/lib combination wants) — a type-only cast, identical bytes either way.
function responseOf(bytes: Uint8Array, init?: ResponseInit): Response {
  return new Response(bytes as unknown as BodyInit, init);
}

describe("createHttpTransport", () => {
  it("POSTs a kebab-cased envelope to the CSIL-RPC mount with the right headers", async () => {
    const request = { domain: "example.org" };
    const response = { redirectUrl: "https://example.org/login" };
    const okBytes = RpcResponse.ok("BeginLoginResponse", toBeginLoginResponseCbor(response)).encode();
    const fetchImpl = fetchReturning(responseOf(okBytes, { status: 200 }));
    const transport = createHttpTransport({ fetchImpl });

    const result = await new AuthAsyncClient(transport).beginLogin(request);

    expect(fetchImpl).toHaveBeenCalledTimes(1);
    const [url, init] = (fetchImpl as ReturnType<typeof vi.fn>).mock.calls[0] as [string, RequestInit];
    expect(url).toBe(RPC_ENDPOINT);
    expect(init.method).toBe("POST");
    expect(init.credentials).toBe("same-origin");
    expect((init.headers as Record<string, string>)["Content-Type"]).toBe("application/cbor");

    const sentEnvelope = RpcRequest.decode(init.body as Uint8Array);
    expect(sentEnvelope.service).toBe("AuthService");
    expect(sentEnvelope.op).toBe("begin-login");
    expect(fromBeginLoginRequestCbor(sentEnvelope.payload)).toEqual(request);

    expect(result).toEqual(response);
  });

  it("throws ServiceErrorRefusal for the ServiceError variant, decoded via the generated codec", async () => {
    const errPayload = toServiceErrorTypeCbor({ code: 5, message: "no board with that slug", resourceType: "board" });
    const bytes = RpcResponse.ok("ServiceError", errPayload).encode();
    const transport = createHttpTransport({ fetchImpl: fetchReturning(responseOf(bytes, { status: 200 })) });

    const err = await new BoardAsyncClient(transport).getBoard("missing").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ServiceErrorRefusal);
    const serviceErr = err as ServiceErrorRefusal;
    expect(serviceErr.value.code).toBe(5);
    expect(serviceErr.value.resourceType).toBe("board");
    expect(serviceErr.value.message).toBe("no board with that slug");
  });

  it("throws FirepitTransportError for a non-zero transport status", async () => {
    const bytes = RpcResponse.transportError(Status.Forbidden, "not a board maintainer").encode();
    const transport = createHttpTransport({ fetchImpl: fetchReturning(responseOf(bytes, { status: 200 })) });

    const err = await transport.call("board", "ArchiveBoard", new Uint8Array()).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(FirepitTransportError);
    const transportErr = err as FirepitTransportError;
    expect(transportErr.status).toBe(Status.Forbidden);
    expect(transportErr.message).toContain("not a board maintainer");
  });

  it("throws FirepitTransportError when the carrier itself fails (network error)", async () => {
    const fetchImpl = vi.fn().mockRejectedValue(new Error("network down")) as unknown as typeof fetch;
    const transport = createHttpTransport({ fetchImpl });

    const err = await transport.call("auth", "Whoami", new Uint8Array()).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(FirepitTransportError);
    expect((err as FirepitTransportError).message).toContain("network down");
  });

  it("throws FirepitTransportError for an undecodable response body", async () => {
    const fetchImpl = fetchReturning(responseOf(new Uint8Array([0xff, 0xff, 0xff]), { status: 200 }));
    const transport = createHttpTransport({ fetchImpl });

    const err = await transport.call("auth", "Whoami", new Uint8Array()).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(FirepitTransportError);
  });
});
