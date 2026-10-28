import { HttpResponse, http } from "msw";
import { NextRequest } from "next/server";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { GET as redeem } from "@/app/auth/handoff/route";
import { GET as start } from "@/app/auth/sign-in/route";
import { HANDOFF_STATE_COOKIE } from "@garam/platform-sdk-ts/handoff";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

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

const STATE = "s".repeat(43);

/** A request to this host's `path`, carrying a state cookie where given. */
function request(path: string, state?: string) {
  const req = new NextRequest(
    new URL(path, "http://console.garam.localhost:3000"),
  );
  if (state !== undefined) req.cookies.set(HANDOFF_STATE_COOKIE, state);
  return req;
}

/** Answers the redemption as garam does, recording what it was sent. */
function redeems(sent: unknown[]) {
  server.use(
    http.post(`${API_URL}/auth/handoff/redeem`, async ({ request }) => {
      sent.push(await request.json());
      return new HttpResponse(null, {
        status: 204,
        headers: {
          "set-cookie":
            "garam_session=handed-off; Path=/; Max-Age=3600; HttpOnly; SameSite=Lax",
        },
      });
    }),
  );
}

describe("starting a handoff", () => {
  it("sends the visitor to the site with a state this host holds, carrying where they were going", () => {
    const response = start(
      request("/auth/sign-in?next=%2Fconsole%2Finvitations%2Ftok-123"),
    );

    expect(response.status).toBe(307);
    const location = new URL(response.headers.get("location") ?? "");
    expect(location.origin + location.pathname).toBe(
      "http://garam.localhost:3001/handoff",
    );
    expect(location.searchParams.get("next")).toBe(
      "/console/invitations/tok-123",
    );
    const state = location.searchParams.get("state");
    expect(state).toMatch(/^[A-Za-z0-9_-]{43}$/);

    // The state the site is sent is the one only the redemption route
    // is sent back.
    const cookie = response.cookies.get(HANDOFF_STATE_COOKIE);
    expect(cookie).toMatchObject({
      value: state,
      path: "/auth/handoff",
      httpOnly: true,
      sameSite: "lax",
      secure: false,
    });
    expect(response.headers.get("referrer-policy")).toBe("no-referrer");
  });

  it("draws a state of its own for every handoff", () => {
    const one = start(request("/auth/sign-in")).cookies.get(
      HANDOFF_STATE_COOKIE,
    );
    const two = start(request("/auth/sign-in")).cookies.get(
      HANDOFF_STATE_COOKIE,
    );
    expect(one?.value).not.toBe(two?.value);
  });

  it("marks the state Secure behind an ingress that terminated TLS", () => {
    const req = request("/auth/sign-in");
    req.headers.set("x-forwarded-proto", "https");
    expect(start(req).cookies.get(HANDOFF_STATE_COOKIE)?.secure).toBe(true);
  });

  // The control for the return path: one off the console is dropped
  // rather than carried to the site and back.
  it("drops a return path that leaves the console", () => {
    const location = new URL(
      start(
        request("/auth/sign-in?next=%2F%2Fevil.example%2Fconsole"),
      ).headers.get("location") ?? "",
    );
    expect(location.searchParams.get("next")).toBe("/console");
  });
});

describe("redeeming a handoff", () => {
  beforeEach(() => {
    jar.planted.length = 0;
    jar.deleted.length = 0;
  });

  it("redeems the code with the state this browser holds and lands where the visitor was going", async () => {
    const sent: unknown[] = [];
    redeems(sent);

    const response = await redeem(
      request("/auth/handoff?code=the-code&next=%2Fconsole%2Forgs", STATE),
    );

    expect(sent).toEqual([{ code: "the-code", state: STATE }]);
    expect(jar.planted).toEqual([
      expect.objectContaining({ name: "garam_session", value: "handed-off" }),
    ]);
    expect(jar.deleted).toEqual([
      { name: HANDOFF_STATE_COOKIE, path: "/auth/handoff" },
    ]);
    expect(response.status).toBe(303);
    expect(response.headers.get("location")).toBe("/console/orgs");
    expect(response.headers.get("referrer-policy")).toBe("no-referrer");
  });

  it("sends a code the backend refuses to sign in on the site, planting nothing", async () => {
    server.use(
      http.post(`${API_URL}/auth/handoff/redeem`, () =>
        HttpResponse.json(
          { kind: "unauthenticated", message: "handoff not live" },
          { status: 401 },
        ),
      ),
    );

    const response = await redeem(
      request("/auth/handoff?code=the-code&next=%2Fconsole%2Forgs", STATE),
    );

    expect(jar.planted).toEqual([]);
    expect(jar.deleted).toHaveLength(1);
    expect(response.headers.get("location")).toBe(
      "http://garam.localhost:3001/login?next=%2Fconsole%2Forgs",
    );
  });

  // Without the state this browser drew, nothing reaches the backend:
  // the code is not this browser's to redeem.
  it("asks the backend nothing for a browser holding no state", async () => {
    const sent: unknown[] = [];
    redeems(sent);

    const response = await redeem(request("/auth/handoff?code=the-code"));

    expect(sent).toEqual([]);
    expect(jar.planted).toEqual([]);
    expect(response.headers.get("location")).toBe(
      "http://garam.localhost:3001/login?next=%2Fconsole",
    );
  });
});
