import { NextRequest } from "next/server";
import { unstable_doesMiddlewareMatch } from "next/experimental/testing/server";
import { afterEach, describe, expect, it, vi } from "vitest";

import { config, proxy } from "@/proxy";

const SITE = "http://garam.localhost:3001";
const CONSOLE = "http://console.garam.localhost:3000";

/**
 * Every page the console served under this host before it moved to its
 * own (ADR-0131), one address each, with its parameters filled in and a
 * query where the page reads one. A bookmark of any of them has to land
 * on the same page at the console's origin.
 */
const BOOKMARKS = [
  "/console",
  "/console/device?code=ABCD-EFGH",
  "/console/invitations/secret-token",
  "/console/orgs",
  "/console/orgs/new",
  "/console/orgs/acme",
  "/console/orgs/acme/agents",
  "/console/orgs/acme/agents/new",
  "/console/orgs/acme/agents/build-bot",
  "/console/orgs/acme/consents",
  "/console/orgs/acme/hosts",
  "/console/orgs/acme/members/alice",
  "/console/orgs/acme/operators",
  "/console/orgs/acme/operators/field-one",
  "/console/orgs/acme/operators/field-one/connect",
  "/console/orgs/acme/people",
  "/console/orgs/acme/people/alice",
  "/console/orgs/acme/permissions",
  "/console/orgs/acme/permissions/new",
  "/console/orgs/acme/permissions/attachments",
  "/console/orgs/acme/permissions/d1e2f3a4b5c6d7e8",
  "/console/orgs/acme/projects",
  "/console/orgs/acme/projects/frontend",
  "/console/orgs/acme/settings",
  "/console/orgs/acme/teams",
  "/console/orgs/acme/teams/new",
  "/console/orgs/acme/teams/eng?tab=members",
  "/console/sessions",
  "/console/a/path/the/console/does/not/have",
  // `garam login` names `/device` relative to the host whose `/api` it
  // was pointed at.
  "/device?code=ABCD-EFGH",
];

describe("proxy", () => {
  it.each(BOOKMARKS)(
    "sends %s to the same address on the console",
    (bookmark) => {
      // Matched first: a path the matcher leaves out never reaches the
      // function, and this site would answer it with its own 404.
      expect(
        unstable_doesMiddlewareMatch({ config, url: `${SITE}${bookmark}` }),
      ).toBe(true);

      const response = proxy(new NextRequest(new URL(bookmark, SITE)));

      expect(response.status).toBe(307);
      expect(response.headers.get("location")).toBe(`${CONSOLE}${bookmark}`);
    },
  );

  it("answers the site's own pages itself", () => {
    // The control for the list above: a matcher that took every path
    // would pass it as well.
    for (const page of ["/", "/login", "/register", "/consoles", "/apiary"]) {
      expect(
        unstable_doesMiddlewareMatch({ config, url: `${SITE}${page}` }),
      ).toBe(false);
    }
  });
});

/**
 * The proxy as a process started with `apiUrl` runs it: `env` is read
 * when the module loads, so each address is a fresh import.
 */
async function proxyServing(apiUrl: string) {
  vi.resetModules();
  vi.stubEnv("GARAM_API_URL", apiUrl);
  return (await import("@/proxy")).proxy;
}

describe("proxy /api/*", () => {
  afterEach(() => vi.unstubAllEnvs());

  it.each([
    ["http://garam-api:8080", "http://garam-api:8080/healthz"],
    ["http://api.other.invalid:9000", "http://api.other.invalid:9000/healthz"],
  ])(
    "sends /api/healthz to the backend at %s, the address the process was given",
    async (apiUrl, want) => {
      const response = (await proxyServing(apiUrl))(
        new NextRequest(new URL("/api/healthz", SITE)),
      );

      // Two addresses through one build is the property: a destination
      // fixed when the code was built would be the same for both.
      expect(response.headers.get("x-middleware-rewrite")).toBe(want);
    },
  );

  it.each([
    ["/api/me?x=1", "/me?x=1"],
    [
      "/api/auth/google/callback?code=a%2Fb",
      "/auth/google/callback?code=a%2Fb",
    ],
    ["/api", "/"],
  ])("sends %s to %s, the backend's own path", async (path, want) => {
    const response = (await proxyServing("http://backend.invalid:8080"))(
      new NextRequest(new URL(path, SITE)),
    );

    expect(response.headers.get("x-middleware-rewrite")).toBe(
      `http://backend.invalid:8080${want}`,
    );
    expect(response.headers.get("location")).toBeNull();
  });

  it("keeps a path prefix the backend's address carries", async () => {
    const response = (await proxyServing("http://gateway.invalid/garam"))(
      new NextRequest(new URL("/api/me", SITE)),
    );

    expect(response.headers.get("x-middleware-rewrite")).toBe(
      "http://gateway.invalid/garam/me",
    );
  });

  it("runs on /api and what is under it, and on no path that only starts with it", () => {
    for (const path of ["/api", "/api/me", "/api/a/b/c"]) {
      expect(
        unstable_doesMiddlewareMatch({ config, url: `${SITE}${path}` }),
      ).toBe(true);
    }
    expect(
      unstable_doesMiddlewareMatch({ config, url: `${SITE}/apiary` }),
    ).toBe(false);
  });
});
