import { HttpResponse, http, type JsonBodyType } from "msw";

import type { User } from "@/features/auth";
import type {
  Agent,
  AgentDefinition,
  Group,
  Member,
  Operator,
  OrgConsent,
  Organization,
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
 * it rather than when it enrolled.
 */
export const fieldOne: Operator = {
  grn: "grn:acme:default:operator:field-one",
  orgGrn: acme.grn,
  registeredBy: alice.grn,
  createdAt: "2026-01-02T03:04:05Z",
  enrollmentState: "enrolled",
  certificateIssuedAt: "2026-03-04T05:06:07Z",
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
  operatorGrn: "grn:acme:default:operator:field-one",
  values: { model: "haiku" },
  definedBy: alice.grn,
  createdAt: "2026-01-02T03:04:05Z",
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
 * Every route the fixtures model: what it answers signed in, and
 * whether the backend guards it. The split is the one
 * `api/openapi.yaml` records — an operation carrying `security: []`
 * answers without a session — and the signed-out fixtures below
 * reproduce it.
 */
const routes: { path: string; guarded: boolean; body: JsonBodyType }[] = [
  { path: "/me", guarded: true, body: alice },
  { path: "/auth/providers", guarded: false, body: ["google"] },
  { path: "/orgs", guarded: true, body: [acme] },
  { path: "/orgs/acme", guarded: true, body: acme },
  { path: "/orgs/acme/groups", guarded: true, body: [engineering] },
  { path: "/orgs/acme/projects", guarded: true, body: [frontend] },
  { path: "/orgs/acme/members", guarded: true, body: [aliceMembership] },
  { path: "/orgs/acme/operators", guarded: true, body: [fieldOne] },
  { path: "/orgs/acme/agents", guarded: true, body: [reportedAgent] },
  { path: "/orgs/acme/agent-definitions", guarded: true, body: [fieldAgent] },
  { path: "/orgs/acme/consents", guarded: true, body: [globexConsent] },
];

/** The signed-in happy path: Alice owns Acme, which holds one of each. */
export const handlers = routes.map(({ path, body }) =>
  http.get(`${API_URL}${path}`, () => HttpResponse.json(body)),
);

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
  return routes
    .filter((route) => route.guarded)
    .map(({ path }) => http.get(`${API_URL}${path}`, noSession));
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
