import { NextRequest } from "next/server";
import { describe, expect, it } from "vitest";

import { proxy } from "@/proxy";
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
  it("sends a visitor without a session cookie to the login page, carrying where they were going", () => {
    const response = proxy(request("/console/orgs?subject=team"));

    expect(response.status).toBe(307);
    expect(response.headers.get("location")).toBe(
      "http://localhost:3000/login?next=%2Fconsole%2Forgs%3Fsubject%3Dteam",
    );
  });

  it("keeps an invitation link's token through the redirect", () => {
    const response = proxy(request("/console/invitations/tok-123"));

    const location = new URL(response.headers.get("location") ?? "");
    expect(location.pathname).toBe("/login");
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
