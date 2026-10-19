import { HttpResponse, http, type JsonBodyType } from "msw";
import { z } from "zod";

import type { PermissionExplanation } from "@/lib/permission-explanation";
import type { User } from "@/features/auth";
import type {
  Agent,
  AgentEnrollmentListing,
  AgentSupervisor,
  AgentDefinition,
  Group,
  GroupMember,
  InvitationPreview,
  Member,
  Operator,
  OperatorKey,
  OrgConsent,
  OrgDeletionPreview,
  Organization,
  Policy,
  PolicyAttachment,
  PolicyVersion,
  Project,
  RegisteredOperator,
} from "@/features/orgs";

/** The backend base URL the tests run the server-side client against. */
export const API_URL = "http://localhost:8080";

/** The signed-in user every test runs as. */
export const alice: User = {
  grn: "grn:root:default:user:alice",
  nickname: "Alice",
};

/** The one organization the fixtures place Alice in. */
export const acme: Organization = {
  grn: "grn:root:default:org:acme",
  name: "Acme",
  createdAt: "2026-01-02T03:04:05Z",
};

/** The single group Acme holds. */
export const engineering: Group = {
  grn: "grn:acme:default:group:eng",
  name: "Engineering",
  orgGrn: acme.grn,
  roles: ["member"],
};

/** The single project Acme owns. */
export const frontend: Project = {
  grn: "grn:acme:default:project:frontend",
  name: "Frontend",
  orgGrn: acme.grn,
  roles: [],
};

/**
 * The single operator Acme has registered, which enrolled: it holds a
 * certificate, and `certificateIssuedAt` is when garam last signed for
 * it rather than when it enrolled. Its name differs from its
 * identifier, so a page showing one where the other belongs is caught.
 */
export const fieldOne: Operator = {
  grn: "grn:acme:default:operator:field-one",
  name: "Field cluster one",
  description: "Runs the batch agents.",
  orgGrn: acme.grn,
  registeredBy: alice.grn,
  createdAt: "2026-01-02T03:04:05Z",
  enrollmentState: "enrolled",
  certificateIssuedAt: "2026-03-04T05:06:07Z",
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

/**
 * What registering `fieldOne` answered: the record and the one-time
 * token beside it. The token is in this answer and in no later read of
 * the record, as it is in garam's (ADR-0064).
 */
export const fieldOneRegistered: RegisteredOperator = {
  operator: {
    ...fieldOne,
    enrollmentState: "pending",
    certificateIssuedAt: undefined,
  },
  enrollment: {
    token: "8f14e45fceea167a5a36dedd4bea2543a1ba1ffc2fbd07a1",
    expiresAt: "2026-01-02T03:14:05Z",
  },
  credential: {
    credential:
      "garam_oc_3c59dc048e8850243be8079a5c74d079c4ca4238a0b923820dcc509a6f75849b",
    issuedAt: "2026-01-02T03:04:05Z",
  },
};

/**
 * The single agent Acme holds. It is not the agent `fieldAgent`
 * describes: a definition's GRN is answered here only once an operator
 * claims it, and nothing joins the two lists.
 */
export const reportedAgent: Agent = {
  grn: "grn:acme:default:agent:1c3d5e7f90a2b4c6",
  orgGrn: acme.grn,
  provisioningState: "failed",
  runtimeState: "disconnected",
  createdAt: "2026-01-02T03:04:05Z",
  updatedAt: "2026-01-03T04:05:06Z",
};

/** The single agent Acme says it should have. */
export const fieldAgent: AgentDefinition = {
  agentGrn: "grn:acme:default:agent:9f2ac1b40d8e7a35",
  orgGrn: acme.grn,
  name: "Scout",
  operatorGrn: "grn:acme:default:operator:field-one",
  values: { model: "haiku" },
  definedBy: alice.grn,
  createdAt: "2026-01-02T03:04:05Z",
  cutover: null,
};

/**
 * The single consent relation Acme is a side of: both halves stand, so
 * messages cross between Acme and Globex. Globex is not an
 * organization Alice belongs to — a consent names one this side cannot
 * otherwise read.
 */
export const globexConsent: OrgConsent = {
  orgGrn: "grn:root:default:org:globex",
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

/**
 * An active policy of Acme attached to Engineering, evaluating the
 * first of its two versions.
 */
export const denyDeletes: Policy = {
  grn: "grn:acme:default:policy:d1e2f3a4b5c6d7e8",
  orgGrn: acme.grn,
  name: "Deny project deletion",
  status: "active",
  defaultVersion: 1,
  createdAt: "2026-01-02T03:04:05Z",
  updatedAt: "2026-01-02T03:04:05Z",
};

/** The two versions `denyDeletes` holds, oldest first. */
export const denyDeletesVersions: PolicyVersion[] = [1, 2].map((version) => ({
  policyGrn: denyDeletes.grn,
  version,
  document: {
    Version: "2026-07-25",
    Statement: [
      {
        Effect: "Deny",
        Action: version === 1 ? ["project:delete"] : ["project:*"],
        Resource: ["grn:acme:*"],
      },
    ],
  },
  createdBy: alice.grn,
  createdAt: "2026-01-02T03:04:05Z",
}));

/** `denyDeletes`'s one attachment, to Engineering. */
export const denyDeletesAttachments: PolicyAttachment[] = [
  {
    policyGrn: denyDeletes.grn,
    subjectGrn: engineering.grn,
    createdAt: "2026-01-02T03:04:05Z",
  },
];

/** An active policy of Acme with no attachment: it reaches everyone. */
export const baselineRead: Policy = {
  grn: "grn:acme:default:policy:0a1b2c3d4e5f6071",
  orgGrn: acme.grn,
  name: "Baseline read",
  status: "active",
  defaultVersion: 1,
  createdAt: "2026-01-02T03:04:05Z",
  updatedAt: "2026-01-02T03:04:05Z",
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
    agentDefinitions: 1,
    agentTemplates: 1,
    operators: 1,
    consents: 1,
  },
};

/** A standalone approval no host has spent yet (ADR-0082). */
export const pendingApproval: AgentEnrollmentListing = {
  id: "0f8c2a5e-9d1b-4c3a-8e7f-6a5b4c3d2e1f",
  placement: "standalone",
  state: "pending",
  agentGrn: "grn:acme:default:agent:5a6b7c8d9e0f1a2b",
  supervisorGrn: "grn:acme:default:supervisor:build-08",
  hostName: "build-08",
  approvedBy: alice.grn,
  createdAt: "2026-10-18T09:00:00Z",
  expiresAt: "2026-10-18T10:00:00Z",
};

/** The supervisor of a standalone host running Scout, at epoch 2. */
export const buildHost: AgentSupervisor = {
  grn: "grn:acme:default:supervisor:build-07",
  agentGrn: "grn:acme:default:agent:9f2ac1b40d8e7a35",
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
 * `api/openapi.yaml` records — an operation carrying `security: []`
 * answers without a session — and the signed-out fixtures below
 * reproduce it.
 */
const routes: { path: string; guarded: boolean; body: JsonBodyType }[] = [
  { path: "/me", guarded: true, body: alice },
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
  { path: "/orgs/acme/operators", guarded: true, body: [fieldOne] },
  { path: "/orgs/acme/operators/field-one", guarded: true, body: fieldOne },
  {
    path: "/orgs/acme/operators/field-one/keys",
    guarded: true,
    body: { keys: [fieldOneKey] },
  },
  { path: "/orgs/acme/agents", guarded: true, body: [reportedAgent] },
  { path: "/orgs/acme/agent-definitions", guarded: true, body: [fieldAgent] },
  {
    path: "/orgs/acme/agent-definitions/9f2ac1b40d8e7a35",
    guarded: true,
    body: fieldAgent,
  },
  { path: "/orgs/acme/consents", guarded: true, body: [globexConsent] },
  {
    path: "/orgs/acme/agent-enrollments",
    guarded: true,
    body: [pendingApproval],
  },
  { path: "/orgs/acme/agent-supervisors", guarded: true, body: [buildHost] },
  {
    path: "/orgs/acme/policies",
    guarded: true,
    body: [denyDeletes, baselineRead],
  },
  {
    path: "/orgs/acme/policies/d1e2f3a4b5c6d7e8/versions",
    guarded: true,
    body: denyDeletesVersions,
  },
  {
    path: "/orgs/acme/policies/d1e2f3a4b5c6d7e8/attachments",
    guarded: true,
    body: denyDeletesAttachments,
  },
  {
    path: "/orgs/acme/policies/0a1b2c3d4e5f6071/versions",
    guarded: true,
    body: [{ ...denyDeletesVersions[0], policyGrn: baselineRead.grn }],
  },
  {
    path: "/orgs/acme/policies/0a1b2c3d4e5f6071/attachments",
    guarded: true,
    body: [],
  },
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
 * Answers Acme's permission checks with `decide(action)` for each pair,
 * in the order asked, as the server does.
 */
export function answerPermissionChecks(
  decide: (action: string) => "allow" | "deny" | "unknown",
) {
  return http.post(
    `${API_URL}/orgs/acme/permission-checks`,
    async ({ request }) => {
      const { checks } = PermissionCheckRequest.parse(await request.json());
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
  basis: "statements",
  statements: [{ effect: "Allow", source: "org-role", roles: ["owner"] }],
};

/** The signed-in happy path: Alice owns Acme, which holds one of each. */
export const handlers = [
  ...routes.map(({ path, body }) =>
    http.get(`${API_URL}${path}`, () => HttpResponse.json(body)),
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
