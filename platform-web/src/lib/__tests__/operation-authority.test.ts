import { afterEach, describe, expect, it, vi } from "vitest";
import { z } from "zod";

import {
  EMPTY_BODY_SHA256,
  OperatorReportsParser,
  TemplateVersionParser,
  callControl,
  sha256Hex,
  type ControlCall,
  type MintInput,
  type MintResult,
} from "@/lib/operation-authority";

const AUTHORITY = "opaque-authority-0123456789";

/** A read of one template version under a prefixed endpoint. */
function readCall(): ControlCall<{ name: string }> {
  return {
    endpoint: "https://control.example.test/garam",
    path: "/v1/orgs/acme/templates/web%20agent/versions/3?view=full",
    method: "GET",
    requestId: "req-1",
    org: "acme",
    audience: "grn:acme:default:operator:hosted",
    operation: "agent-template:read",
    target: "grn:root:default:org:acme",
    parser: z.object({ name: z.string() }),
  };
}

function minting(result: MintResult = { status: "ok", authority: AUTHORITY }) {
  return vi.fn<(input: MintInput) => Promise<MintResult>>(async () => result);
}

function answering(response: Response | Error) {
  const fetch = vi.fn<typeof globalThis.fetch>(async () => {
    if (response instanceof Error) throw response;
    return response;
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("callControl", () => {
  it("binds the target of the very URL it sends, and sends the authority only in its header", async () => {
    const mint = minting();
    const fetch = answering(Response.json({ name: "web agent" }));

    const result = await callControl(readCall(), mint);

    expect(result).toEqual({ status: "ok", data: { name: "web agent" } });
    const minted = mint.mock.calls[0]![0];
    expect(minted.requestTarget).toBe(
      "/garam/v1/orgs/acme/templates/web%20agent/versions/3?view=full",
    );
    expect(minted.bodySha256).toBe(EMPTY_BODY_SHA256);
    const [url, init] = fetch.mock.calls[0]!;
    expect(url).toBeInstanceOf(URL);
    const sent = url as URL;
    expect(sent.origin).toBe("https://control.example.test");
    expect(sent.pathname + sent.search).toBe(minted.requestTarget);
    expect(sent.href).not.toContain(AUTHORITY);
    expect(init?.headers).toEqual({
      authorization: `Garam-Operation ${AUTHORITY}`,
    });
    expect(init?.redirect).toBe("error");
    expect(init?.credentials).toBe("omit");
    expect(init?.body).toBeUndefined();
  });

  it("binds a POST to the digest of the exact bytes it sends", async () => {
    const mint = minting();
    const fetch = answering(Response.json({ name: "web", version: 4 }));
    const body = { requestId: "req-2", profile: { name: "small", version: 1 } };

    await callControl(
      {
        ...readCall(),
        path: "/v1/orgs/acme/templates/web/versions",
        method: "POST",
        body,
        requestId: "req-2",
        operation: "agent-template:publish",
        parser: z.unknown(),
      },
      mint,
    );

    const sent = fetch.mock.calls[0]![1]!.body;
    expect(typeof sent).toBe("string");
    expect(mint.mock.calls[0]![0].bodySha256).toBe(
      await sha256Hex(sent as string),
    );
    expect(mint.mock.calls[0]![0].requestTarget).toBe(
      "/garam/v1/orgs/acme/templates/web/versions",
    );
  });

  it("binds no request target for a create, which binds only its body", async () => {
    const mint = minting();
    answering(
      Response.json({
        agent: "grn:acme:default:agent:1",
        revision: "1",
        epoch: "1",
      }),
    );

    await callControl(
      {
        ...readCall(),
        path: "/v1/orgs/acme/agents",
        method: "POST",
        body: { requestId: "req-3" },
        requestId: "req-3",
        operation: "agent:create",
        target: "grn:acme:default:operator:field-one",
        parser: z.unknown(),
      },
      mint,
    );

    expect(mint.mock.calls[0]![0]).not.toHaveProperty("requestTarget");
  });

  it("sends nothing when garam refuses the mint", async () => {
    const fetch = answering(Response.json({ name: "web agent" }));

    const refused = await callControl(
      readCall(),
      minting({ status: "refused", message: "not delegated" }),
    );

    expect(refused).toEqual({
      status: "refused",
      refusal: { source: "mint", sentence: "not delegated" },
    });
    expect(fetch).not.toHaveBeenCalled();
    // The control: the same call under a minted authority is sent.
    await callControl(readCall(), minting());
    expect(fetch).toHaveBeenCalledOnce();
  });

  it("reads a mint that threw as unavailable and sends nothing", async () => {
    const fetch = answering(Response.json({ name: "web agent" }));
    const throwing = vi.fn<(input: MintInput) => Promise<MintResult>>(
      async () => {
        throw new Error("the Server Action failed");
      },
    );

    expect(await callControl(readCall(), throwing)).toEqual({
      status: "unavailable",
    });
    expect(fetch).not.toHaveBeenCalled();
  });

  it("reads a failed connection and a 503 as unavailable", async () => {
    answering(new TypeError("Failed to fetch"));
    expect(await callControl(readCall(), minting())).toEqual({
      status: "unavailable",
    });
    answering(new Response(null, { status: 503 }));
    expect(await callControl(readCall(), minting())).toEqual({
      status: "unavailable",
    });
  });

  it("carries a refusal's status and kind, and not its message", async () => {
    answering(
      Response.json(
        { kind: "embedding_required", message: "the model needs one" },
        { status: 400 },
      ),
    );
    expect(await callControl(readCall(), minting())).toEqual({
      status: "refused",
      refusal: { source: "control", status: 400, kind: "embedding_required" },
    });
    answering(
      Response.json({ message: "request target differs" }, { status: 403 }),
    );
    expect(await callControl(readCall(), minting())).toEqual({
      status: "refused",
      refusal: { source: "control", status: 403, kind: null },
    });
  });

  it("reads an empty kind, and a body that is not one, as no kind", async () => {
    answering(Response.json({ kind: "", message: "no" }, { status: 409 }));
    expect(await callControl(readCall(), minting())).toEqual({
      status: "refused",
      refusal: { source: "control", status: 409, kind: null },
    });
    answering(new Response("<html>bad gateway</html>", { status: 502 }));
    expect(await callControl(readCall(), minting())).toEqual({
      status: "refused",
      refusal: { source: "control", status: 502, kind: null },
    });
  });

  it("refuses an answer outside the agreed contract", async () => {
    answering(Response.json({ title: "web agent" }));
    expect(await callControl(readCall(), minting())).toEqual({
      status: "refused",
      refusal: { source: "unreadable" },
    });
  });
});

describe("TemplateVersionParser", () => {
  const version = (model: Record<string, unknown>) => ({
    name: "web agent",
    version: 1,
    profile: { name: "small", version: 1 },
    configuration: { model, ego: "Answers.", tools: {} },
  });
  const model = {
    provider: "openai-compatible",
    baseUrl: "https://api.example.test/v1",
    name: "MiniMax-M2",
    apiKeyRef: "model-keys/web",
  };

  it("keeps the model's embedding, so a version published from it carries it", () => {
    const embedding = {
      baseUrl: "http://tei.embeddings.svc/v1",
      name: "BAAI/bge-base-en-v1.5",
      apiKeyRef: "",
    };

    const parsed = TemplateVersionParser.parse(
      version({ ...model, embedding }),
    );

    expect(parsed.configuration.model.embedding).toEqual(embedding);
  });

  it("leaves a model that names no embedding without one", () => {
    const parsed = TemplateVersionParser.parse(version(model));

    expect(parsed.configuration.model).not.toHaveProperty("embedding");
  });
});

describe("OperatorReportsParser", () => {
  const reported = {
    grn: "grn:acme:default:operator:field-one",
    images: [
      {
        name: "manager",
        ref: "ghcr.io/garamsh/operator:v0.4.0",
        digest: `sha256:${"ab".repeat(32)}`,
      },
      { name: "agent", ref: "ghcr.io/garamsh/gagent:v1.2.0" },
    ],
    reportedAt: "2026-10-14T08:00:00Z",
    lastContactAt: "2026-10-15T09:30:00Z",
  };

  it("reads the agreed answer, keeping each digest where one was given", () => {
    expect(OperatorReportsParser.parse({ operators: [reported] })).toEqual({
      operators: [reported],
    });
  });

  it("leaves the times out of an operator that never reported or made contact, rather than filling them", () => {
    const parsed = OperatorReportsParser.parse({
      operators: [{ grn: reported.grn, images: [] }],
    });

    expect(parsed.operators[0]).not.toHaveProperty("reportedAt");
    expect(parsed.operators[0]).not.toHaveProperty("lastContactAt");
  });

  it("refuses an entry with no images list", () => {
    // The control: the same entry with its list is read.
    expect(
      OperatorReportsParser.safeParse({
        operators: [{ grn: reported.grn, images: [] }],
      }).success,
    ).toBe(true);
    expect(
      OperatorReportsParser.safeParse({ operators: [{ grn: reported.grn }] })
        .success,
    ).toBe(false);
  });
});
