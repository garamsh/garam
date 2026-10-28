import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { GaramContractError } from "@/lib/garam";
import {
  MAX_PERMISSION_CHECKS,
  type PermissionCheck,
  checkPermissions,
} from "@/lib/permission-check";
import {
  API_URL,
  answerPermissionChecks,
  signedOut,
} from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

const CHECKS = `${API_URL}/orgs/acme/permission-checks`;

describe("checkPermissions", () => {
  it("answers each action by the name the page gave it", async () => {
    server.use(
      answerPermissionChecks((action) =>
        action === "operator:read"
          ? "allow"
          : action === "agent:create"
            ? "unknown"
            : "deny",
      ),
    );

    const permissions = await checkPermissions("acme", {
      register: { action: "operator:register" },
      read: { action: "operator:read" },
      create: { action: "agent:create" },
    });

    expect(permissions.get("register")).toBe("denied");
    expect(permissions.get("read")).toBe("allowed");
    // The server cannot decide every action, and says so per pair.
    expect(permissions.get("create")).toBe("unknown");
  });

  it("sends every pair in one request, resources named by identifier", async () => {
    const sent: unknown[] = [];
    server.use(
      http.post(CHECKS, async ({ request }) => {
        sent.push(await request.json());
        return HttpResponse.json({
          results: [
            { action: "group:update", decision: "allow" },
            { action: "org:read", decision: "allow" },
          ],
        });
      }),
    );

    await checkPermissions("acme", {
      rename: {
        action: "group:update",
        resource: { type: "group", id: "eng" },
      },
      read: { action: "org:read" },
    });

    expect(sent).toEqual([
      {
        checks: [
          { action: "group:update", resource: { type: "group", id: "eng" } },
          { action: "org:read" },
        ],
      },
    ]);
  });

  it.each([
    ["the API is unreachable", () => HttpResponse.error()],
    [
      "a dependency is down",
      () => HttpResponse.json({ message: "internal error" }, { status: 503 }),
    ],
    [
      "the server refuses the check",
      () => HttpResponse.json({ message: "no" }, { status: 403 }),
    ],
  ])("answers unknown when %s", async (_, answer) => {
    server.use(http.post(CHECKS, answer));

    const permissions = await checkPermissions("acme", {
      register: { action: "operator:register" },
    });

    expect(permissions.get("register")).toBe("unknown");

    // The control: the same check, answered, is a decision, so
    // `unknown` above is the failure's and not the helper's default.
    server.use(answerPermissionChecks(() => "deny"));
    const answered = await checkPermissions("acme", {
      register: { action: "operator:register" },
    });
    expect(answered.get("register")).toBe("denied");
  });

  it("sends a rejected session to the login page", async () => {
    server.use(...signedOut());

    await expect(
      checkPermissions("acme", { register: { action: "operator:register" } }),
    ).rejects.toThrow("NEXT_REDIRECT:/login");
  });

  it.each([
    ["does not pair with what was asked", { results: [] }],
    [
      "carries a decision the contract does not name",
      { results: [{ action: "operator:register", decision: "maybe" }] },
    ],
  ])("answers unknown, and logs it, when the answer %s", async (_, body) => {
    const logged = vi.spyOn(console, "error").mockImplementation(() => {});
    server.use(http.post(CHECKS, () => HttpResponse.json(body)));

    const permissions = await checkPermissions("acme", {
      register: { action: "operator:register" },
    });

    expect(permissions.get("register")).toBe("unknown");
    expect(logged).toHaveBeenCalledWith(expect.any(GaramContractError));
    logged.mockRestore();

    // The control: a well-formed answer is read, so `unknown` above
    // is the malformed body's and not the helper's default.
    server.use(answerPermissionChecks(() => "deny"));
    const answered = await checkPermissions("acme", {
      register: { action: "operator:register" },
    });
    expect(answered.get("register")).toBe("denied");
  });

  it(`asks at most ${MAX_PERMISSION_CHECKS} actions, and nothing at all for none`, async () => {
    const asks = (n: number) =>
      Object.fromEntries(
        Array.from({ length: n }, (_, i): [string, PermissionCheck] => [
          `action-${i}`,
          { action: "org:read" },
        ]),
      );

    const fifty = await checkPermissions("acme", asks(MAX_PERMISSION_CHECKS));
    expect(fifty.get("action-49")).toBe("allowed");
    await expect(
      checkPermissions("acme", asks(MAX_PERMISSION_CHECKS + 1)),
    ).rejects.toThrow(/at most 50/);

    let requests = 0;
    server.use(
      http.post(CHECKS, () => {
        requests += 1;
        return HttpResponse.json({ results: [] });
      }),
    );
    await checkPermissions("acme", {});
    expect(requests).toBe(0);
  });
});
