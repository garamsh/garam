import { NextRequest } from "next/server";
import { unstable_doesMiddlewareMatch } from "next/experimental/testing/server";
import { describe, expect, it } from "vitest";

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
    for (const page of ["/", "/login", "/register", "/consoles", "/api/me"]) {
      expect(
        unstable_doesMiddlewareMatch({ config, url: `${SITE}${page}` }),
      ).toBe(false);
    }
  });
});
