// @vitest-environment node
import { NextRequest } from "next/server";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { GET as redeem } from "@/app/auth/handoff/route";
import { GET as start } from "@/app/auth/sign-in/route";
import { HANDOFF_STATE_COOKIE } from "@garam/platform-sdk-ts/handoff";

// The cookie jar the redemption writes through; it records what was
// planted and what was deleted.
const jar = vi.hoisted(() => ({
  planted: [] as Record<string, unknown>[],
  deleted: [] as unknown[],
}));
vi.mock("next/headers", () => ({
  cookies: () =>
    Promise.resolve({
      get: () => undefined,
      set: (options: Record<string, unknown>) => {
        jar.planted.push(options);
      },
      delete: (options: unknown) => {
        jar.deleted.push(options);
      },
    }),
}));
vi.mock("server-only", () => ({}));

const STATE = "s".repeat(43);
const SITE = "http://garam.localhost:3001";

/** A request to this host's `path`, carrying a state cookie where given. */
function request(path: string, state?: string) {
  const req = new NextRequest(
    new URL(path, "http://cowork.garam.localhost:3100"),
  );
  if (state !== undefined) req.cookies.set(HANDOFF_STATE_COOKIE, state);
  return req;
}

/** Answers the redemption with `response`, recording what it was sent. */
function redeems(sent: unknown[], response: Response) {
  vi.stubGlobal(
    "fetch",
    async (url: string, init: { body: string }): Promise<Response> => {
      sent.push({ url, body: JSON.parse(init.body) });
      return response;
    },
  );
}

const issued = () =>
  new Response(null, {
    status: 204,
    headers: {
      "set-cookie":
        "garam_session=handed-off; Path=/; Max-Age=3600; HttpOnly; SameSite=Lax",
    },
  });

beforeEach(() => {
  jar.planted.length = 0;
  jar.deleted.length = 0;
});
afterEach(() => {
  vi.unstubAllGlobals();
});

describe("starting a handoff", () => {
  it("sends the visitor to the site naming cowork, with a state this host holds", () => {
    const response = start(request("/auth/sign-in?next=%2Facme%2Fpage-1"));

    const location = new URL(response.headers.get("location") ?? "");
    expect(location.origin + location.pathname).toBe(`${SITE}/handoff`);
    expect(location.searchParams.get("app")).toBe("cowork");
    expect(location.searchParams.get("next")).toBe("/acme/page-1");
    const cookie = response.cookies.get(HANDOFF_STATE_COOKIE);
    expect(cookie?.value).toBe(location.searchParams.get("state"));
    expect(cookie).toMatchObject({
      path: "/auth/handoff",
      httpOnly: true,
      sameSite: "lax",
    });
    expect(response.headers.get("set-cookie")).not.toMatch(/domain=/i);
    expect(response.headers.get("referrer-policy")).toBe("no-referrer");
  });

  it("drops a return path off this host", () => {
    const response = start(request("/auth/sign-in?next=%2F%2Fevil.example"));

    const location = new URL(response.headers.get("location") ?? "");
    expect(location.searchParams.get("next")).toBe("/");
  });

  it("marks the state cookie Secure behind a gateway that terminated TLS", () => {
    const req = request("/auth/sign-in");
    req.headers.set("x-forwarded-proto", "https");

    expect(start(req).cookies.get(HANDOFF_STATE_COOKIE)?.secure).toBe(true);
  });
});

describe("redeeming a handoff", () => {
  it("plants the session the platform issued and lands on the path", async () => {
    const sent: unknown[] = [];
    redeems(sent, issued());

    const response = await redeem(
      request("/auth/handoff?code=the-code&next=%2Facme", STATE),
    );

    expect(sent).toEqual([
      {
        url: "http://localhost:8080/auth/handoff/redeem",
        body: { code: "the-code", state: STATE },
      },
    ]);
    expect(jar.planted).toEqual([
      expect.objectContaining({ name: "garam_session", value: "handed-off" }),
    ]);
    expect(jar.planted[0]).not.toHaveProperty("domain");
    expect(jar.deleted).toEqual([
      { name: HANDOFF_STATE_COOKIE, path: "/auth/handoff" },
    ]);
    expect(response.status).toBe(303);
    expect(response.headers.get("location")).toBe("/acme");
    expect(response.headers.get("referrer-policy")).toBe("no-referrer");
  });

  // `%2Facme` above is this test's control: the same redemption,
  // landing on the path it was given.
  it.each([
    "%2F%2Fevil.example%2Fsteal",
    "https%3A%2F%2Fevil.example%2Fsteal",
    "%2F%5Cevil.example%2Fsteal",
    "%2Facme%2F..%2Fsteal",
  ])(
    "lands on cowork's home for a return path the rule drops: %s",
    async (next) => {
      redeems([], issued());

      const response = await redeem(
        request(`/auth/handoff?code=the-code&next=${next}`, STATE),
      );

      expect(response.status).toBe(303);
      expect(response.headers.get("location")).toBe("/");
    },
  );

  it.each([
    ["past Latin-1", "%2Facme%2F%ED%95%9C", "/acme/%ED%95%9C"],
    ["percent-encoded", "%2Facme%2Fa%2520b%3Fq%3D1", "/acme/a%20b?q=1"],
    ["with a fragment", "%2Facme%23%ED%95%9C", "/acme#%ED%95%9C"],
  ])("writes a return path %s as a header holds it", async (_, next, want) => {
    redeems([], issued());

    const response = await redeem(
      request(`/auth/handoff?code=the-code&next=${next}`, STATE),
    );

    expect(response.headers.get("location")).toBe(want);
  });

  it("sends a refused redemption to sign in on the site, carrying cowork", async () => {
    redeems([], new Response(null, { status: 401 }));

    const response = await redeem(
      request("/auth/handoff?code=spent&next=%2Facme", STATE),
    );

    expect(jar.planted).toEqual([]);
    const location = new URL(response.headers.get("location") ?? "");
    expect(location.origin + location.pathname).toBe(`${SITE}/login`);
    expect(location.searchParams.get("app")).toBe("cowork");
    expect(location.searchParams.get("next")).toBe("/acme");
  });

  it("asks nothing of the platform for a browser holding no state", async () => {
    const sent: unknown[] = [];
    redeems(sent, issued());

    const response = await redeem(request("/auth/handoff?code=the-code"));

    expect(sent).toEqual([]);
    expect(jar.planted).toEqual([]);
    expect(response.headers.get("location")).toContain("/login");
  });

  it("throws a platform failure rather than reading it as a refusal", async () => {
    redeems([], new Response(null, { status: 503 }));

    await expect(
      redeem(request("/auth/handoff?code=the-code", STATE)),
    ).rejects.toThrow("503");
  });
});
