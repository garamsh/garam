import { HttpResponse, http } from "msw";
import { NextRequest } from "next/server";
import { describe, expect, it } from "vitest";

import { GET } from "@/app/handoff/route";
import { SESSION_COOKIE } from "@garam/platform-sdk-ts/session";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

const STATE = "s".repeat(43);

/** A request to the site's `path`, signed in here where asked. */
function request(path: string, { signedIn = true } = {}) {
  const req = new NextRequest(new URL(path, "http://garam.localhost:3001"));
  if (signedIn) req.cookies.set(SESSION_COOKIE, "test-session-token");
  return req;
}

/** Answers `POST /auth/handoff` as garam does, recording what was sent. */
function issues(sent: unknown[]) {
  server.use(
    http.post(`${API_URL}/auth/handoff`, async ({ request }) => {
      sent.push({
        body: await request.json(),
        cookie: request.headers.get("cookie"),
      });
      return HttpResponse.json({ code: "the-code" }, { status: 201 });
    }),
  );
}

describe("the site's handoff", () => {
  it("hands a visitor signed in here to the console with a code bound to the console's state", async () => {
    const sent: unknown[] = [];
    issues(sent);

    const response = await GET(
      request(
        `/handoff?next=%2Fconsole%2Finvitations%2Ftok-123&state=${STATE}`,
      ),
    );

    expect(sent).toEqual([
      { body: { state: STATE }, cookie: "garam_session=test-session-token" },
    ]);
    expect(response.status).toBe(303);
    const location = new URL(response.headers.get("location") ?? "");
    expect(location.origin + location.pathname).toBe(
      "http://console.garam.localhost:3000/auth/handoff",
    );
    expect(location.searchParams.get("code")).toBe("the-code");
    expect(location.searchParams.get("next")).toBe(
      "/console/invitations/tok-123",
    );
    // The state stays out of the address the code travels in: the
    // console holds it, and the pair is what redeems.
    expect(location.searchParams.has("state")).toBe(false);
    expect(response.headers.get("referrer-policy")).toBe("no-referrer");
  });

  it("asks a visitor not signed in here to sign in first, carrying where they were going", async () => {
    const sent: unknown[] = [];
    issues(sent);

    const response = await GET(
      request(`/handoff?next=%2Fconsole%2Forgs&state=${STATE}`, {
        signedIn: false,
      }),
    );

    expect(sent).toEqual([]);
    expect(response.headers.get("location")).toBe(
      "/login?next=%2Fconsole%2Forgs",
    );
  });

  it("asks a visitor whose session the backend rejects to sign in again", async () => {
    server.use(
      http.post(`${API_URL}/auth/handoff`, () =>
        HttpResponse.json(
          { kind: "unauthenticated", message: "session" },
          { status: 401 },
        ),
      ),
    );

    const response = await GET(
      request(`/handoff?next=%2Fconsole%2Forgs&state=${STATE}`),
    );

    expect(response.headers.get("location")).toBe(
      "/login?next=%2Fconsole%2Forgs",
    );
  });

  // A request the console did not start carries no state it drew, so
  // nothing is issued and the console is asked to start over.
  it("issues nothing for a request carrying no state, and sends it to the console to start over", async () => {
    const sent: unknown[] = [];
    issues(sent);

    const response = await GET(
      request("/handoff?next=%2Fconsole%2Forgs&state=short"),
    );

    expect(sent).toEqual([]);
    expect(response.headers.get("location")).toBe(
      "http://console.garam.localhost:3000/auth/sign-in?next=%2Fconsole%2Forgs",
    );
  });

  it("hands a visitor signed in here to cowork, whatever host the request names", async () => {
    const sent: unknown[] = [];
    issues(sent);

    const response = await GET(
      request(`/handoff?app=cowork&next=%2Facme%2Fpage-1&state=${STATE}`),
    );

    expect(sent).toEqual([
      { body: { state: STATE }, cookie: "garam_session=test-session-token" },
    ]);
    const location = new URL(response.headers.get("location") ?? "");
    expect(location.origin + location.pathname).toBe(
      "http://cowork.garam.localhost:3100/auth/handoff",
    );
    expect(location.searchParams.get("next")).toBe("/acme/page-1");
    expect(response.headers.get("referrer-policy")).toBe("no-referrer");
  });

  it("carries cowork through a sign-in", async () => {
    const response = await GET(
      request(`/handoff?app=cowork&next=%2Facme&state=${STATE}`, {
        signedIn: false,
      }),
    );

    const location = new URL(
      response.headers.get("location") ?? "",
      "http://garam.localhost:3001",
    );
    expect(location.pathname).toBe("/login");
    expect(location.searchParams.get("app")).toBe("cowork");
    expect(location.searchParams.get("next")).toBe("/acme");
  });

  it("asks cowork to start over for a request carrying no state", async () => {
    const response = await GET(request("/handoff?app=cowork&next=%2Facme"));

    expect(response.headers.get("location")).toBe(
      "http://cowork.garam.localhost:3100/auth/sign-in?next=%2Facme",
    );
  });

  // The console's constraint is the console's: a path cowork accepts
  // is dropped for the console, and the same path on cowork is the
  // control.
  it("reads a return path by the app it is for", async () => {
    issues([]);

    const response = await GET(request(`/handoff?next=%2Facme&state=${STATE}`));

    const location = new URL(response.headers.get("location") ?? "");
    expect(location.searchParams.get("next")).toBe("/console");
  });

  // An origin in the request decides nothing, and an app off the list
  // is refused rather than read as the console.
  it.each(["evil", "https://evil.example", ""])(
    "refuses app=%j and issues nothing",
    async (app) => {
      const sent: unknown[] = [];
      issues(sent);

      const response = await GET(
        request(
          `/handoff?app=${encodeURIComponent(app)}&next=%2Fconsole&state=${STATE}`,
        ),
      );

      expect(sent).toEqual([]);
      expect(response.status).toBe(400);
      expect(response.headers.get("location")).toBeNull();
    },
  );

  it("drops a return path that leaves the console", async () => {
    issues([]);

    const response = await GET(
      request(`/handoff?next=%2F%2Fevil.example&state=${STATE}`),
    );

    const location = new URL(response.headers.get("location") ?? "");
    expect(location.searchParams.get("next")).toBe("/console");
  });
});
