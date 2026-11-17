import { HttpResponse, http, type JsonBodyType } from "msw";
import { z } from "zod";

import { parseGrn } from "@/lib/grn";
import type { PermissionExplanation } from "@/lib/permission-explanation";
import type { components } from "@garam/platform-sdk-ts/api";
import type { Session, User } from "@/features/auth";
import type {
  Agent,
  AgentEnrollmentListing,
  AgentSupervisor,
  Group,
  GroupMember,
  InvitationPreview,
  AuditRecord,
  AuditRecordPage,
  Member,
  Operator,
  OperatorKey,
  OrgConsent,
  OrgDeletionPreview,
  Organization,
  IamPolicy,
  IamPolicyDetail,
  Project,
} from "@/features/orgs";

type OrgSearch = components["schemas"]["OrgSearch"];
type FoundKind = components["schemas"]["FoundKind"]["kind"];

/** The backend base URL the tests run the server-side client against. */
export const API_URL = "http://localhost:8080";

/** The signed-in user every test runs as. */
export const alice: User = {
  grn: "grn:root/iam/user/alice",
  nickname: "Alice",
};

/** The one organization the fixtures place Alice in. */
export const acme: Organization = {
  grn: "grn:root/iam/org/acme",
  name: "Acme",
  createdAt: "2026-01-02T03:04:05Z",
};

/** The single group Acme holds. */
export const engineering: Group = {
  grn: "grn:acme/iam/group/eng",
  name: "Engineering",
  orgGrn: acme.grn,
  roles: ["member"],
};

/** The single project Acme owns. */
export const frontend: Project = {
  grn: "grn:acme/iam/project/frontend",
  name: "Frontend",
  orgGrn: acme.grn,
  roles: [],
};

/**
 * The single operator Acme has registered. Its name differs from its
 * identifier, so a page showing one where the other belongs is caught.
 */
export const fieldOne: Operator = {
  grn: "grn:acme/iam/operator/field-one",
  name: "Field cluster one",
  description: "Runs the batch agents.",
  orgGrn: acme.grn,
  registeredBy: alice.grn,
  createdAt: "2026-01-02T03:04:05Z",
};

/**
 * The one key `fieldOne` holds: active and already used to sign in, so
 * a page reading it has a live key with a last exchange to show.
 */
export const fieldOneKey: OperatorKey = {
  kid: "Xq3vNw2dZr5gP0tB8mKcYh7LfA1uJe9sRiOoVnT4Wy6",
  algorithm: "RS256",
  state: "active",
  issuedAt: "2026-10-01T09:00:00Z",
  lastExchangedAt: "2026-10-14T08:30:00Z",
};

/** The single agent Acme holds. */
export const reportedAgent: Agent = {
  grn: "grn:acme/iam/agent/1c3d5e7f90a2b4c6",
  orgGrn: acme.grn,
  provisioningState: "failed",
  runtimeState: "disconnected",
  createdAt: "2026-01-02T03:04:05Z",
  updatedAt: "2026-01-03T04:05:06Z",
};

/**
 * The single consent relation Acme is a side of: both halves stand, so
 * messages cross between Acme and Globex. Globex is not an
 * organization Alice belongs to — a consent names one this side cannot
 * otherwise read.
 */
export const globexConsent: OrgConsent = {
  orgGrn: "grn:root/iam/org/globex",
  grantedHere: true,
  grantedThere: true,
  stands: true,
};

/**
 * Alice's membership of Acme, at the role that may invite. `nickname`
 * is her user row's, which is what the members table shows: a fixture
 * without one cannot tell a name from the identifier segment.
 */
export const aliceMembership: Member = {
  userGrn: alice.grn,
  nickname: alice.nickname,
  role: "owner",
  since: "2026-01-02T03:04:05Z",
};

/**
 * Who Engineering holds: Alice and Acme's reported agent, one of each
 * type a team can hold.
 */
export const engineeringMembers: GroupMember[] = [
  { memberGrn: alice.grn },
  { memberGrn: reportedAgent.grn },
];

/** The identifier of `denyDeletes`, as the policy service issues it. */
export const DENY_DELETES_ID = "p-d1e2f3a4b5c6d7e8d1e2f3a4b5c6d7e8";

/** The identifier of `engineeringAdmins`. */
export const ENGINEERING_ADMINS_ID = "p-0a1b2c3d4e5f60710a1b2c3d4e5f6071";

/** An active guardrail of Acme, deciding by its first of two versions. */
export const denyDeletes: IamPolicyDetail = {
  policy: {
    id: DENY_DELETES_ID,
    name: "Deny project deletion",
    kind: "guardrail",
    status: "active",
    defaultVersion: 1,
  },
  versions: [1, 2].map((version) => ({
    version,
    body: {
      kind: "guardrail",
      text: `forbid (principal, action == Action::"${
        version === 1 ? "iam:project.delete" : "iam:project.update"
      }", resource);`,
    },
    createdBy: alice.grn,
  })),
};

/** Alice's write of a new version of `denyDeletes`, as the audit reads it. */
export const aliceWrite: AuditRecord = {
  id: 7,
  kind: "write",
  firstAt: "2026-05-02T09:30:00Z",
  lastAt: "2026-05-02T09:30:00Z",
  count: 1,
  service: "platform",
  subject: alice.grn,
  action: "iam:policy.update",
  resource: `grn:acme/iam/policy/${DENY_DELETES_ID}`,
  outcome: "created",
  policyIds: [`grn:acme/iam/policy/${DENY_DELETES_ID}`],
  versionFrom: 1,
  versionTo: 2,
};

/** A member's refused read, collapsed with its repeats. */
export const collapsedDenial: AuditRecord = {
  id: 6,
  kind: "denial",
  firstAt: "2026-05-02T09:00:00Z",
  lastAt: "2026-05-02T09:10:00Z",
  count: 3,
  service: "platform",
  subject: "grn:root/iam/user/bob",
  action: "iam:project.delete",
  resource: "grn:acme/iam/project/frontend",
  outcome: "deny",
  policyIds: [`grn:acme/iam/policy/${DENY_DELETES_ID}`],
  versionFrom: 0,
  versionTo: 0,
};

/** The audit records Acme holds, newest first. */
export const acmeAuditPage: AuditRecordPage = {
  records: [aliceWrite, collapsedDenial],
};

/** A draft role link: Engineering is admin of the frontend project. */
export const engineeringAdmins: IamPolicyDetail = {
  policy: {
    id: ENGINEERING_ADMINS_ID,
    name: "Engineering admins",
    kind: "role-link",
    status: "draft",
    defaultVersion: 1,
  },
  versions: [
    {
      version: 1,
      body: {
        kind: "role-link",
        roleLink: {
          role: "admin",
          scope: "team-or-project",
          principal: engineering.grn,
          resource: frontend.grn,
        },
      },
      createdBy: alice.grn,
    },
  ],
};

/** Every policy Acme holds, read through the list. */
export const acmePolicies: IamPolicy[] = [denyDeletes, engineeringAdmins].map(
  ({ policy }) => policy,
);

/**
 * A policy of the earlier engine, which the search still finds and the
 * permissions pages no longer open.
 */
const earlierEnginePolicy = {
  grn: "grn:acme/iam/policy/d1e2f3a4b5c6d7e8",
  name: "Deny project deletion",
};

/**
 * A live invitation into Acme, read with the token `secret-token`. Its
 * org name differs from the identifier, so a page addressing the org
 * by the one where the other belongs is caught.
 */
export const acmeInvitation: InvitationPreview = {
  org: "acme",
  orgName: "Acme",
  role: "admin",
  state: "pending",
  expiresAt: "2026-01-09T03:04:05Z",
};

/** What deleting Acme would take: one of most kinds, and its CA. */
export const acmeDeletionPreview: OrgDeletionPreview = {
  certificateAuthority: true,
  counts: {
    groups: 1,
    projects: 1,
    members: 1,
    invitations: 0,
    policies: 2,
    agents: 1,
    operators: 1,
    consents: 1,
  },
};

/** A standalone approval no host has spent yet (ADR-0082). */
export const pendingApproval: AgentEnrollmentListing = {
  id: "0f8c2a5e-9d1b-4c3a-8e7f-6a5b4c3d2e1f",
  placement: "standalone",
  state: "pending",
  agentGrn: "grn:acme/iam/agent/5a6b7c8d9e0f1a2b",
  supervisorGrn: "grn:acme/iam/supervisor/build-08",
  hostName: "build-08",
  approvedBy: alice.grn,
  createdAt: "2026-10-18T09:00:00Z",
  expiresAt: "2026-10-18T10:00:00Z",
};

/** The supervisor of a standalone host running Scout, at epoch 2. */
export const buildHost: AgentSupervisor = {
  grn: "grn:acme/iam/supervisor/build-07",
  agentGrn: "grn:acme/iam/agent/9f2ac1b40d8e7a35",
  enrollmentId: "1a2b3c4d-5e6f-4a1b-8c2d-3e4f5a6b7c8d",
  placement: "standalone",
  state: "active",
  hostName: "build-07",
  epoch: 2,
  registeredAt: "2026-10-17T08:00:00Z",
  lastActivatedAt: "2026-10-18T08:30:00Z",
};

/**
 * Every route the fixtures model: what it answers signed in, and
 * whether the backend guards it. The split is the one
 * `platform/api/openapi.yaml` records — an operation carrying
 * `security: []` answers without a session — and the signed-out
 * fixtures below reproduce it.
 */
/**
 * Alice's sessions: the browser this request was made on, and a CLI
 * she signed in from a laptop (ADR-0120). Newest first, as garam
 * answers them.
 */
export const aliceSessions: Session[] = [
  {
    id: "s-cli",
    kind: "cli",
    name: "alice-laptop",
    createdAt: "2026-10-20T09:00:00Z",
    expiresAt: "2026-11-19T09:00:00Z",
    current: false,
  },
  {
    id: "s-browser",
    kind: "browser",
    createdAt: "2026-10-20T08:00:00Z",
    expiresAt: "2026-10-27T08:00:00Z",
    current: true,
  },
];

const routes: { path: string; guarded: boolean; body: JsonBodyType }[] = [
  { path: "/me", guarded: true, body: alice },
  { path: "/me/sessions", guarded: true, body: aliceSessions },
  { path: "/auth/providers", guarded: false, body: ["google"] },
  {
    path: "/invitations/secret-token",
    guarded: false,
    body: acmeInvitation,
  },
  { path: "/orgs", guarded: true, body: [acme] },
  { path: "/orgs/acme", guarded: true, body: acme },
  {
    path: "/orgs/acme/deletion-preview",
    guarded: true,
    body: acmeDeletionPreview,
  },
  { path: "/orgs/acme/groups", guarded: true, body: [engineering] },
  { path: "/orgs/acme/groups/eng", guarded: true, body: engineering },
  {
    path: "/orgs/acme/groups/eng/members",
    guarded: true,
    body: engineeringMembers,
  },
  { path: "/orgs/acme/projects", guarded: true, body: [frontend] },
  { path: "/orgs/acme/projects/frontend", guarded: true, body: frontend },
  {
    path: "/orgs/acme/projects/frontend/members",
    guarded: true,
    body: [{ memberGrn: alice.grn }],
  },
  { path: "/orgs/acme/members", guarded: true, body: [aliceMembership] },
  { path: "/orgs/acme/invitations", guarded: true, body: [] },
  { path: "/orgs/acme/operators", guarded: true, body: [fieldOne] },
  { path: "/orgs/acme/operators/field-one", guarded: true, body: fieldOne },
  {
    path: "/orgs/acme/operators/field-one/keys",
    guarded: true,
    body: { keys: [fieldOneKey] },
  },
  { path: "/orgs/acme/agents", guarded: true, body: [reportedAgent] },
  { path: "/orgs/acme/consents", guarded: true, body: [globexConsent] },
  {
    path: "/orgs/acme/agent-enrollments",
    guarded: true,
    body: [pendingApproval],
  },
  { path: "/orgs/acme/agent-supervisors", guarded: true, body: [buildHost] },
  { path: "/orgs/acme/iam-policies", guarded: true, body: acmePolicies },
  { path: "/orgs/acme/audit-records", guarded: true, body: acmeAuditPage },
  ...[denyDeletes, engineeringAdmins].map((detail) => ({
    path: `/orgs/acme/iam-policies/${detail.policy.id}`,
    guarded: true,
    body: detail,
  })),
];

const PermissionCheckRequest = z.object({
  checks: z.array(
    z.object({
      action: z.string(),
      resource: z.object({ type: z.string(), id: z.string() }).optional(),
    }),
  ),
});

/**
 * The action the platform answers only on the organization and refuses
 * on a policy (permission_check.go), with the whole request. Every
 * other policy write it answers on either.
 */
const ON_THE_ORGANIZATION_ONLY = ["iam:permit.write"];

/**
 * Answers Acme's permission checks with `decide(action)` for each pair,
 * in the order asked, as the server does, and refuses a request that
 * pairs `iam:permit.write` with a resource, 400.
 */
export function answerPermissionChecks(
  decide: (action: string) => "allow" | "deny" | "unknown",
) {
  return http.post(
    `${API_URL}/orgs/acme/permission-checks`,
    async ({ request }) => {
      const { checks } = PermissionCheckRequest.parse(await request.json());
      const misplaced = checks.find(
        (check) =>
          ON_THE_ORGANIZATION_ONLY.includes(check.action) &&
          check.resource !== undefined,
      );
      if (misplaced) {
        return HttpResponse.json(
          {
            kind: "invalid_argument",
            message: `${misplaced.action} is not checked on a ${misplaced.resource?.type}`,
          },
          { status: 400 },
        );
      }
      return HttpResponse.json({
        results: checks.map((check) => ({
          ...check,
          decision: decide(check.action),
        })),
      });
    },
  );
}

/**
 * Answers Acme's permission explanations with `explain(action)`, as
 * garam does for the one pair the query names.
 */
export function answerExplanations(
  explain: (
    action: string,
  ) => Omit<PermissionExplanation, "action" | "resource">,
) {
  return http.get(
    `${API_URL}/orgs/acme/permission-explanation`,
    ({ request }) => {
      const action = new URL(request.url).searchParams.get("action") ?? "";
      return HttpResponse.json({ action, ...explain(action) });
    },
  );
}

/** Alice's owner role allows an action, as garam explains it. */
export const ownerAllows: Omit<PermissionExplanation, "action" | "resource"> = {
  decision: "allow",
  basis: "policy-service",
  grounds: [
    {
      kind: "role-link",
      policy: "link-owner-0123456789abcdef0123456789abcdef",
      role: "owner",
      path: "organization",
    },
  ],
};

/** The signed-in happy path: Alice owns Acme, which holds one of each. */
/**
 * What `searchOrg` answers over the fixtures above: acme's objects of
 * each kind whose name or identifier holds `q`, case aside, at most
 * `limit` of each with the count of the rest. Another organization
 * holds none.
 */
function searchFixtures(org: string, q: string, limit: number): OrgSearch {
  const kinds: [FoundKind, { grn: string; name?: string }[]][] = [
    [
      "person",
      [{ grn: aliceMembership.userGrn, name: aliceMembership.nickname }],
    ],
    ["agent", [{ grn: reportedAgent.grn }]],
    ["team", [engineering]],
    ["policy", [earlierEnginePolicy]],
    ["project", [frontend]],
  ];
  const needle = q.trim().toLowerCase();
  const holds = (text: string | undefined) =>
    text !== undefined && text.toLowerCase().includes(needle);
  return {
    found: kinds.map(([kind, objects]) => {
      const matched =
        org === "acme"
          ? objects
              .filter(
                ({ grn, name }) =>
                  holds(name) || holds(parseGrn(grn)?.identifier),
              )
              .map(({ grn, name }) => ({ grn, name }))
          : [];
      return {
        kind,
        objects: matched.slice(0, limit),
        more: Math.max(0, matched.length - limit),
      };
    }),
  };
}

/** The identifier the mock issues to the policy a test creates. */
export const CREATED_POLICY_ID = "p-c0ffee00c0ffee00c0ffee00c0ffee00";

const PolicyWrite = z.object({
  name: z.string().optional(),
  body: z.object({
    kind: z.enum(["guardrail", "permit", "role-link"]),
    text: z.string().optional(),
    roleLink: z
      .object({
        role: z.string(),
        scope: z.enum(["organization", "team-or-project"]),
        principal: z.string(),
        resource: z.string(),
      })
      .optional(),
  }),
});

function invalid(message: string) {
  return HttpResponse.json(
    { kind: "invalid_argument", message },
    { status: 400 },
  );
}

/**
 * What the platform refuses of a body (ADR-0169), 400 with the reason:
 * Cedar the service cannot parse, a role link its membership derives, a
 * link whose principal is its resource, and a body missing its half.
 */
function refusedBody(body: z.infer<typeof PolicyWrite>["body"]) {
  if (body.kind === "role-link") {
    const link = body.roleLink;
    if (link === undefined) return "a role link carries its roleLink";
    if (
      link.scope === "organization" &&
      parseGrn(link.principal)?.serviceType === "user"
    ) {
      return "a user's organization role is its membership's and is not written here";
    }
    if (link.scope === "team-or-project" && link.principal === link.resource) {
      return "a team's or project's own scoped link is derived from it and is not written here";
    }
    return null;
  }
  if (body.text === undefined || body.text.trim() === "") {
    return "a guardrail or a permit carries its Cedar text";
  }
  if (!body.text.includes("(")) {
    return "unexpected token at line 1: expected a policy";
  }
  return null;
}

export const handlers = [
  ...routes.map(({ path, body }) =>
    http.get(`${API_URL}${path}`, () => HttpResponse.json(body)),
  ),
  http.get(`${API_URL}/orgs/:org/search`, ({ params, request }) => {
    const query = new URL(request.url).searchParams;
    return HttpResponse.json(
      searchFixtures(
        String(params.org),
        query.get("q") ?? "",
        Number(query.get("limit") ?? 5),
      ),
    );
  }),
  http.post(`${API_URL}/orgs/acme/iam-policies`, async ({ request }) => {
    const { name, body } = PolicyWrite.parse(await request.json());
    const refusal = refusedBody(body);
    if (name === undefined || name.trim() === "")
      return invalid("a policy is named");
    if (refusal !== null) return invalid(refusal);
    return HttpResponse.json(
      {
        id: CREATED_POLICY_ID,
        name,
        kind: body.kind,
        status: "draft",
        defaultVersion: 1,
      } satisfies IamPolicy,
      { status: 201 },
    );
  }),
  http.post(
    `${API_URL}/orgs/acme/iam-policies/:policy/versions`,
    async ({ params, request }) => {
      const detail = [denyDeletes, engineeringAdmins].find(
        ({ policy }) => policy.id === params.policy,
      );
      if (detail === undefined) {
        return HttpResponse.json(
          { kind: "not_found", message: "no such policy" },
          { status: 404 },
        );
      }
      const { body } = PolicyWrite.parse(await request.json());
      if (body.kind !== detail.policy.kind) {
        return invalid(
          `a ${detail.policy.kind} takes a ${detail.policy.kind} body`,
        );
      }
      const refusal = refusedBody(body);
      if (refusal !== null) return invalid(refusal);
      return HttpResponse.json(
        { version: detail.versions.length + 1 },
        { status: 201 },
      );
    },
  ),
  // An owner may take every action, by the owner role.
  answerPermissionChecks(() => "allow"),
  answerExplanations(() => ownerAllows),
  http.get(`${API_URL}/orgs/acme/permission-holders`, () =>
    HttpResponse.json({
      holders: [{ type: "user", grn: alice.grn, nickname: alice.nickname }],
    }),
  ),
];

/** What the backend answers on a route it guards, without a session. */
function noSession() {
  return HttpResponse.json(
    { kind: "unauthenticated", message: "no session" },
    { status: 401 },
  );
}

/**
 * Signed out: every route the backend guards refuses, and the public
 * ones keep answering. A route the fixtures do not model is left
 * unhandled, so a page reading one fails by name rather than on a 401
 * nothing decided.
 */
export function signedOut() {
  return [
    ...routes
      .filter((route) => route.guarded)
      .map(({ path }) => http.get(`${API_URL}${path}`, noSession)),
    http.post(`${API_URL}/orgs/acme/permission-checks`, noSession),
  ];
}

/**
 * Signed out, with the backend advertising the given providers.
 * `/auth/providers` is public, so what it answers is the login page's
 * input whether or not the caller holds a session.
 */
export function signedOutWith(providers: string[]) {
  return [
    ...signedOut(),
    http.get(`${API_URL}/auth/providers`, () => HttpResponse.json(providers)),
  ];
}
