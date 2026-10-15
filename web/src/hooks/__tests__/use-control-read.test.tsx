import { renderHook, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";
import { z } from "zod";

import { useControlRead } from "@/hooks/use-control-read";
import type { MintInput, MintResult } from "@/lib/operation-authority";
import { server } from "@/testing/setup";

const ENDPOINT = "https://control.example.test/garam";
const WHERE = {
  endpoint: ENDPOINT,
  audience: "grn:acme:default:operator:hosted",
  path: "/v1/orgs/acme/things",
  target: "grn:root:default:org:acme",
};
const Things = z.object({ things: z.array(z.string()) });

/** A mint that answers `result` and keeps what it was asked. */
function mintAnswering(result: MintResult) {
  const asked: MintInput[] = [];
  const mint = async (input: MintInput) => {
    asked.push(input);
    return result;
  };
  return { mint, asked };
}

describe("useControlRead", () => {
  it("reads the route under the authority the given mint answers, and says nothing of a refused mint but its sentence", async () => {
    const authorizations: (string | null)[] = [];
    server.use(
      http.get(`${ENDPOINT}/v1/orgs/acme/things`, ({ request }) => {
        authorizations.push(request.headers.get("authorization"));
        return HttpResponse.json({ things: ["one"] });
      }),
    );

    // The control: a mint that answers an authority reaches the data.
    const granted = mintAnswering({ status: "ok", authority: "minted" });
    const read = renderHook(() =>
      useControlRead("acme", "operator:read", WHERE, Things, granted.mint),
    );
    await waitFor(() =>
      expect(read.result.current).toEqual({
        status: "ok",
        data: { things: ["one"] },
      }),
    );
    expect(authorizations).toEqual(["Garam-Operation minted"]);
    expect(granted.asked).toEqual([
      expect.objectContaining({
        org: "acme",
        operation: "operator:read",
        audience: WHERE.audience,
        target: WHERE.target,
        requestTarget: "/garam/v1/orgs/acme/things",
      }),
    ]);

    const refused = mintAnswering({
      status: "refused",
      message: "garam refused this.",
    });
    const denied = renderHook(() =>
      useControlRead("acme", "operator:read", WHERE, Things, refused.mint),
    );
    await waitFor(() =>
      expect(denied.result.current).toEqual({
        status: "refused",
        refusal: { source: "mint", sentence: "garam refused this." },
      }),
    );
    // A refused mint sends nothing to the control service.
    expect(authorizations).toHaveLength(1);
  });

  it("asks no mint and answers null where there is nowhere to read", () => {
    const { mint, asked } = mintAnswering({ status: "ok", authority: "x" });
    const { result } = renderHook(() =>
      useControlRead("acme", "operator:read", null, Things, mint),
    );

    expect(result.current).toBeNull();
    expect(asked).toEqual([]);
  });
});
