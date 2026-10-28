import { NextRequest } from "next/server";
import { unstable_doesMiddlewareMatch } from "next/experimental/testing/server";
import { describe, expect, it } from "vitest";

import { config, proxy } from "@/proxy";
import { RETURN_PATH_HEADER } from "@garam/platform-client/return-path";
import { SESSION_COOKIE } from "@garam/platform-client/session";

function request(path: string, { signedIn = false } = {}) {
  const req = new NextRequest(new URL(path, "http://localhost:3000"));
  if (signedIn) {
    req.cookies.set(SESSION_COOKIE, "token");
  }
  return req;
}

describe("proxy", () => {
  it("starts a sign-in handoff for a visitor without a session cookie, carrying where they were going", () => {
    const response = proxy(request("/console/orgs?subject=team"));

    // The cookie is host-only (ADR-0131): this host starts the handoff
    // that brings a session from the site.
    expect(response.status).toBe(307);
    expect(response.headers.get("location")).toBe(
      "http://localhost:3000/auth/sign-in?next=%2Fconsole%2Forgs%3Fsubject%3Dteam",
    );
  });

  it.each([
    "/login",
    "/login?next=%2Fconsole%2Finvitations%2Ftok-123",
    "/register?next=%2Fconsole%2Finvitations%2Ftok-123",
  ])("sends %s, which the site now answers, to the site", (path) => {
    expect(
      unstable_doesMiddlewareMatch({
        config,
        url: `http://localhost:3000${path}`,
      }),
    ).toBe(true);

    const response = proxy(request(path, { signedIn: true }));

    expect(response.status).toBe(307);
    expect(response.headers.get("location")).toBe(
      `http://garam.localhost:3001${path}`,
    );
  });

  it("opens the console at its own root", () => {
    expect(
      unstable_doesMiddlewareMatch({ config, url: "http://localhost:3000/" }),
    ).toBe(true);

    const response = proxy(request("/"));

    expect(response.headers.get("location")).toBe(
      "http://localhost:3000/console",
    );
  });

  it("keeps an invitation link's token through the redirect", () => {
    const response = proxy(request("/console/invitations/tok-123"));

    const location = new URL(response.headers.get("location") ?? "");
    expect(location.origin).toBe("http://localhost:3000");
    expect(location.pathname).toBe("/auth/sign-in");
    expect(location.searchParams.get("next")).toBe(
      "/console/invitations/tok-123",
    );
  });

  it("lets a visitor carrying a session cookie through", () => {
    const response = proxy(request("/console/orgs", { signedIn: true }));

    // Passing through is the property; carrying no Location header is
    // its consequence, and an error response has none either.
    expect(response.status).toBe(200);
    expect(response.headers.get("x-middleware-next")).toBe("1");
    expect(response.headers.get("location")).toBeNull();
  });

  it("stamps a request carrying a cookie with the path it asked for, over any a client sent", () => {
    const req = request("/console/invitations/tok-123?x=1", { signedIn: true });
    req.headers.set(RETURN_PATH_HEADER, "/console/elsewhere");

    const response = proxy(req);

    // Next.js forwards an overridden request header as
    // `x-middleware-request-<name>` on the response it passes through.
    expect(
      response.headers.get(`x-middleware-request-${RETURN_PATH_HEADER}`),
    ).toBe("/console/invitations/tok-123?x=1");
  });
});
