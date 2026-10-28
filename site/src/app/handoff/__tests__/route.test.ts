import { HttpResponse, http } from "msw";
import { NextRequest } from "next/server";
import { describe, expect, it } from "vitest";

import { GET } from "@/app/handoff/route";
import { SESSION_COOKIE } from "@garam/platform-client/session";
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

  it("drops a return path that leaves the console", async () => {
    issues([]);

    const response = await GET(
      request(`/handoff?next=%2F%2Fevil.example&state=${STATE}`),
    );

    const location = new URL(response.headers.get("location") ?? "");
    expect(location.searchParams.get("next")).toBe("/console");
  });
});
