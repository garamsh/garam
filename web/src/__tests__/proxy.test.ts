import { NextRequest } from "next/server";
import { describe, expect, it } from "vitest";

import { proxy } from "@/proxy";
import { SESSION_COOKIE } from "@/lib/session";

function request(path: string, { signedIn = false } = {}) {
  const req = new NextRequest(new URL(path, "http://localhost:3000"));
  if (signedIn) {
    req.cookies.set(SESSION_COOKIE, "token");
  }
  return req;
}

describe("proxy", () => {
  it("sends a visitor without a session cookie to the login page", () => {
    const response = proxy(request("/console/orgs"));

    expect(response.status).toBe(307);
    expect(response.headers.get("location")).toBe(
      "http://localhost:3000/login",
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
});
