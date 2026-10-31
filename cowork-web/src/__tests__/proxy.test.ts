// @vitest-environment node
import { unstable_doesMiddlewareMatch } from "next/experimental/testing/server";
import { NextRequest } from "next/server";
import { describe, expect, it } from "vitest";

import { config, proxy } from "@/proxy";
import { RETURN_PATH_HEADER } from "@garam/platform-sdk-ts/return-path";
import { SESSION_COOKIE } from "@garam/platform-sdk-ts/session";

const HOST = "http://cowork.garam.localhost:3100";

describe("the routes the proxy runs on", () => {
  it.each(["/healthz", "/readyz", "/auth/sign-in", "/auth/handoff"])(
    "leaves %s to a visitor without a session",
    (url) => {
      expect(unstable_doesMiddlewareMatch({ config, url })).toBe(false);
    },
  );

  it.each(["/acme", "/healthz-co", "/healthz/page", "/readyzen"])(
    "runs on %s, a path that only begins like a probe or is a workspace's",
    (url) => {
      expect(unstable_doesMiddlewareMatch({ config, url })).toBe(true);
    },
  );
});

describe("the proxy", () => {
  it("starts a handoff for a visitor without a session, carrying the address", () => {
    const response = proxy(new NextRequest(`${HOST}/acme/tasks?view=board`));

    expect(response.status).toBe(307);
    expect(response.headers.get("location")).toBe(
      `${HOST}/auth/sign-in?next=%2Facme%2Ftasks%3Fview%3Dboard`,
    );
  });

  it("lets a visitor holding a cookie through, stamping the path for a refusal", () => {
    const request = new NextRequest(`${HOST}/acme`);
    request.cookies.set(SESSION_COOKIE, "token");

    const response = proxy(request);

    expect(response.headers.get("location")).toBeNull();
    expect(
      response.headers.get(`x-middleware-request-${RETURN_PATH_HEADER}`),
    ).toBe("/acme");
  });
});
