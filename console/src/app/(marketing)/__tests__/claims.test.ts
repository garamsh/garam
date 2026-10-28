import { existsSync, readFileSync, readdirSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

type Fact =
  | { kind: "path"; path: string; exists: boolean }
  | { kind: "migration"; table: string }
  | { kind: "adr"; id: string; status: string };

type Claim = {
  /** Verbatim as page.tsx states it; line wraps are ignored. */
  prose: string[];
  /** The same claim as the page's tests pin it: web-relative file and text. */
  pinned: { file: string; text: string }[];
  /** The repository facts the claim stands on. */
  facts: Fact[];
};

const UNIT_TEST = "src/app/(marketing)/__tests__/page.test.tsx";
const E2E_SPEC = "e2e/public.spec.ts";

/**
 * The landing page's existence-claims — what it says exists and what it
 * says does not — each bound to the repository facts that make it true
 * (issue #193). Three drifts fail here: the page stops saying a claim,
 * the page's own tests stop pinning it, or the facts stop holding. A
 * claim with no checkable fact does not belong in this table; name it
 * in the pull request that changes it instead.
 */
const CLAIMS: Claim[] = [
  {
    prose: ["an agent already has an identity"],
    pinned: [],
    facts: [
      { kind: "path", path: "internal/agent", exists: true },
      { kind: "migration", table: "agents" },
    ],
  },
  {
    prose: [
      "is a subject of the same policies",
      "Attaching one to a user, a group, or an agent decides who it reaches",
    ],
    pinned: [],
    facts: [
      { kind: "adr", id: "0008", status: "accepted" },
      { kind: "adr", id: "0024", status: "accepted" },
    ],
  },
  {
    prose: ["exchanges messages on a listener of its own"],
    pinned: [],
    facts: [
      { kind: "path", path: "internal/machine", exists: true },
      { kind: "path", path: "internal/machineapi", exists: true },
      { kind: "migration", table: "messages" },
    ],
  },
  {
    prose: [
      "the adapter that would run beside it is not built.",
      "the adapter that would run beside the agent",
    ],
    pinned: [
      { file: UNIT_TEST, text: "the adapter that would run beside the agent" },
    ],
    facts: [{ kind: "path", path: "internal/adapter", exists: false }],
  },
  {
    prose: ["The listener and the message queue are built."],
    pinned: [],
    facts: [
      { kind: "path", path: "internal/machine", exists: true },
      { kind: "path", path: "internal/machineapi", exists: true },
      { kind: "path", path: "internal/message", exists: true },
      { kind: "migration", table: "messages" },
    ],
  },
  {
    prose: ["Reachable, not yet watched", "any way to watch one work"],
    pinned: [
      { file: E2E_SPEC, text: "Reachable, not yet watched" },
      { file: UNIT_TEST, text: "Reachable, not yet watched" },
    ],
    facts: [
      // Watching's two ends are a console route and the adapter
      // (overview.md §Out of scope); garam's half, internal/watch, is
      // built and is not what this claim is about. The console's half
      // is a route that watches one agent. An agent's own page now
      // exists and shows what was requested and reported (ADR-0075)
      // but watches nothing, so the path standing for this claim is a
      // watch below it.
      {
        kind: "path",
        path: "web/src/app/console/orgs/[org]/agents/[agent]/watch",
        exists: false,
      },
      { kind: "path", path: "internal/adapter", exists: false },
    ],
  },
  {
    prose: [
      "a policy attached to it narrows that with a Deny, and no policy widens it past the organization (ADR-0024)",
    ],
    pinned: [],
    facts: [{ kind: "adr", id: "0024", status: "accepted" }],
  },
  {
    prose: ["ADR-0012"],
    pinned: [],
    facts: [{ kind: "adr", id: "0012", status: "accepted" }],
  },
  {
    prose: ["ADR-0013"],
    pinned: [],
    facts: [{ kind: "adr", id: "0013", status: "accepted" }],
  },
  {
    prose: ["ADR-0014"],
    pinned: [],
    facts: [{ kind: "adr", id: "0014", status: "accepted" }],
  },
  {
    prose: [
      "all carry a GRN",
      "One name shape, five segments",
      "A GRN names every resource in the system.",
    ],
    pinned: [{ file: UNIT_TEST, text: "One name shape, five segments" }],
    facts: [
      { kind: "path", path: "internal/grn", exists: true },
      { kind: "path", path: "docs/architecture/grn-model.md", exists: true },
    ],
  },
  {
    prose: [
      "Invitation, not signup",
      "An organization grows by invitation, never by open signup.",
    ],
    pinned: [{ file: UNIT_TEST, text: "Invitation, not signup" }],
    facts: [
      { kind: "path", path: "internal/org", exists: true },
      { kind: "migration", table: "invitations" },
    ],
  },
  {
    prose: ["Groups bundle people; a project holds a unit of work."],
    pinned: [],
    facts: [
      { kind: "migration", table: "groups" },
      { kind: "migration", table: "projects" },
    ],
  },
  {
    prose: [
      "IAM-style documents, stored per organization and versioned append-only.",
    ],
    pinned: [],
    facts: [
      { kind: "path", path: "internal/authz", exists: true },
      { kind: "migration", table: "policies" },
      { kind: "migration", table: "policy_attachments" },
      { kind: "migration", table: "policy_versions" },
    ],
  },
  {
    prose: [
      "In the console today",
      "The organizations you belong to, and for each one its teams, people, agents, projects, operators, consents, permissions, and settings.",
    ],
    pinned: [],
    facts: [
      "teams",
      "people",
      "agents",
      "projects",
      "operators",
      "consents",
      "permissions",
      "settings",
    ].map((section) => ({
      kind: "path" as const,
      path: `web/src/app/console/orgs/[org]/${section}`,
      exists: true,
    })),
  },
];

const REPO_ROOT = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../../../../..",
);
const WEB_ROOT = path.join(REPO_ROOT, "web");

function checkFact(fact: Fact): string | null {
  switch (fact.kind) {
    case "path": {
      const found = existsSync(path.join(REPO_ROOT, fact.path));
      return found === fact.exists
        ? null
        : `${fact.path} ${fact.exists ? "no longer exists" : "exists now"}`;
    }
    case "migration": {
      const creates = readdirSync(path.join(REPO_ROOT, "migrations"))
        .filter((file) => file.endsWith(".up.sql"))
        .some((file) =>
          new RegExp(`\\b${fact.table}\\b`).test(
            readFileSync(path.join(REPO_ROOT, "migrations", file), "utf8"),
          ),
        );
      return creates ? null : `no migration names ${fact.table}`;
    }
    case "adr": {
      const file = readdirSync(
        path.join(REPO_ROOT, "docs/architecture/adr"),
      ).find((entry) => entry.startsWith(`${fact.id}-`));
      if (!file) return `ADR-${fact.id} does not exist`;
      const status = readFileSync(
        path.join(REPO_ROOT, "docs/architecture/adr", file),
        "utf8",
      )
        .split("\n")
        .find((line) => line.startsWith("> Status:"));
      return status?.startsWith(`> Status: ${fact.status}`)
        ? null
        : `ADR-${fact.id} status is "${status ?? "missing"}", expected "${fact.status}"`;
    }
  }
}

describe("landing page existence claims", () => {
  it("states each claim in the page's own words", () => {
    const page = readFileSync(
      path.join(WEB_ROOT, "src/app/(marketing)/page.tsx"),
      "utf8",
    ).replace(/\s+/g, " ");

    for (const claim of CLAIMS) {
      for (const fragment of claim.prose) {
        expect(page, `the page no longer says: ${fragment}`).toContain(
          fragment,
        );
      }
    }
  });

  it("pins each claim in the page's tests as the table records it", () => {
    for (const claim of CLAIMS) {
      for (const { file, text } of claim.pinned) {
        const spec = readFileSync(path.join(WEB_ROOT, file), "utf8");

        expect(spec, `${file} no longer pins: ${text}`).toContain(text);
      }
    }
  });

  it("holds each claim's facts in the repository", () => {
    for (const claim of CLAIMS) {
      for (const fact of claim.facts) {
        const drift = checkFact(fact);

        expect(
          drift,
          `the claim "${claim.prose[0]}" drifted: ${drift ?? ""}`,
        ).toBeNull();
      }
    }
  });
});
