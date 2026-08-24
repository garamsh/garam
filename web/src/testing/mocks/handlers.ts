import { HttpResponse, http } from "msw";

import type { User } from "@/features/auth";
import type {
  AgentDefinition,
  Group,
  Member,
  Organization,
  Project,
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

/** The single agent Acme says it should have. */
export const fieldAgent: AgentDefinition = {
  agentGrn: "grn:acme:default:agent:9f2ac1b40d8e7a35",
  orgGrn: acme.grn,
  operatorGrn: "grn:acme:default:operator:field-one",
  values: { model: "haiku" },
  definedBy: alice.grn,
  createdAt: "2026-01-02T03:04:05Z",
};

/** Alice's membership of Acme, at the role that may invite. */
export const aliceMembership: Member = {
  userGrn: alice.grn,
  role: "owner",
  since: "2026-01-02T03:04:05Z",
};

/** The signed-in happy path: Alice owns Acme, which holds one of each. */
export const handlers = [
  http.get(`${API_URL}/me`, () => HttpResponse.json(alice)),
  http.get(`${API_URL}/auth/providers`, () => HttpResponse.json(["google"])),
  http.get(`${API_URL}/orgs`, () => HttpResponse.json([acme])),
  http.get(`${API_URL}/orgs/acme`, () => HttpResponse.json(acme)),
  http.get(`${API_URL}/orgs/acme/groups`, () =>
    HttpResponse.json([engineering]),
  ),
  http.get(`${API_URL}/orgs/acme/projects`, () =>
    HttpResponse.json([frontend]),
  ),
  http.get(`${API_URL}/orgs/acme/members`, () =>
    HttpResponse.json([aliceMembership]),
  ),
  http.get(`${API_URL}/orgs/acme/agent-definitions`, () =>
    HttpResponse.json([fieldAgent]),
  ),
];

/** Everything the backend guards answers 401 without a session. */
export function unauthenticated() {
  return http.get(`${API_URL}/*`, () =>
    HttpResponse.json(
      { kind: "unauthenticated", message: "no session" },
      {
        status: 401,
      },
    ),
  );
}

/**
 * Signed-out variant for pages that also read public endpoints:
 * `/me` rejects the session while `/auth/providers` still advertises
 * the given providers.
 */
export function signedOutWith(providers: string[]) {
  return [
    http.get(`${API_URL}/me`, () =>
      HttpResponse.json(
        { kind: "unauthenticated", message: "no session" },
        { status: 401 },
      ),
    ),
    http.get(`${API_URL}/auth/providers`, () => HttpResponse.json(providers)),
  ];
}
