import { describe, expect, it } from "vitest";

import { isUnsent, refuseUnsent } from "@/lib/unsent-action";

/** The rejection Next.js gives a Server Action call answered by a 503. */
const transportFailure = () =>
  new Error("An unexpected response was received from the server.");

describe("refuseUnsent", () => {
  it("answers a call garam's answer never came back from as a refusal", async () => {
    const action = refuseUnsent(async (): Promise<{ status: "idle" }> => {
      throw transportFailure();
    }, "garam didn't answer.");

    await expect(action({ status: "idle" }, undefined)).resolves.toEqual({
      status: "error",
      message: "garam didn't answer.",
    });
  });

  it("passes an answer through unchanged", async () => {
    // The control: only a rejection is turned into a refusal.
    const action = refuseUnsent(async () => ({ status: "done" as const }));

    await expect(action({ status: "done" }, undefined)).resolves.toEqual({
      status: "done",
    });
  });
});

describe("isUnsent", () => {
  it("keeps the action's own navigation and a server-side throw as throws", () => {
    const redirect = Object.assign(new Error("NEXT_REDIRECT"), {
      digest: "NEXT_REDIRECT;push;/console/orgs/acme/operators/a/connect;307;",
    });
    expect(() => isUnsent(redirect)).toThrow(redirect);

    // An error the action threw on the server reaches the client with a
    // digest: a bug, for error.tsx rather than a refusal.
    const bug = Object.assign(new Error("boom"), { digest: "1234567890" });
    expect(isUnsent(bug)).toBe(false);

    expect(isUnsent(transportFailure())).toBe(true);
    expect(isUnsent(new TypeError("Failed to fetch"))).toBe(true);
  });
});
