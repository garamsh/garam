import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";

import { getOperatorDelegation } from "@/features/orgs/api/delegation";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

const DELEGATION = `${API_URL}/orgs/acme/operators/hosted/delegation`;

function grantOf(operations: string[]) {
  return {
    grantId: "g-1",
    operatorGrn: "grn:acme:default:operator:hosted",
    controllers: [],
    operations,
    expiresAt: "2026-11-01T00:00:00Z",
    grantedBy: "grn:root:default:user:alice",
    grantedAt: "2026-10-14T00:00:00Z",
  };
}

describe("getOperatorDelegation", () => {
  it("reads a grant that delegates operator:read", async () => {
    server.use(
      http.get(DELEGATION, () =>
        HttpResponse.json(grantOf(["agent-template:read", "operator:read"])),
      ),
    );

    const read = await getOperatorDelegation("acme", "hosted");

    expect(read).toEqual({
      status: "ok",
      data: grantOf(["agent-template:read", "operator:read"]),
    });

    // The control: an operation the contract does not name is outside
    // it, so the read above is the parser accepting operator:read and
    // not one accepting anything.
    server.use(
      http.get(DELEGATION, () =>
        HttpResponse.json(grantOf(["operator:frobnicate"])),
      ),
    );
    expect(await getOperatorDelegation("acme", "hosted")).toEqual({
      status: "unreachable",
    });
  });
});
