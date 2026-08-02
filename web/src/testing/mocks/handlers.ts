import { HttpResponse, http } from "msw";

import type {
  Group,
  Member,
  Organization,
  Project,
} from "@/features/orgs/api/orgs";
import type { User } from "@/features/auth/api/get-session";

/** The backend base URL the tests run the server-side client against. */
export const API_URL = "http://localhost:8080";

export const alice: User = {
  grn: "grn:root:default:user:alice",
  nickname: "Alice",
};

export const acme: Organization = {
  grn: "grn:root:default:org:acme",
  name: "Acme",
  createdAt: "2026-01-02T03:04:05Z",
};

export const engineering: Group = {
  grn: "grn:acme:default:group:eng",
  name: "Engineering",
  orgGrn: acme.grn,
  roles: ["member"],
};

export const frontend: Project = {
  grn: "grn:acme:default:project:frontend",
  name: "Frontend",
  orgGrn: acme.grn,
  roles: [],
};

export const aliceMembership: Member = {
  userGrn: alice.grn,
  role: "owner",
  since: "2026-01-02T03:04:05Z",
};

/** The signed-in happy path: Alice owns Acme, which holds one of each. */
export const handlers = [
  http.get(`${API_URL}/me`, () => HttpResponse.json(alice)),
  http.get(`${API_URL}/orgs`, () => HttpResponse.json([acme])),
  http.get(`${API_URL}/orgs/acme`, () => HttpResponse.json(acme)),
  http.get(`${API_URL}/orgs/acme/groups`, () => HttpResponse.json([engineering])),
  http.get(`${API_URL}/orgs/acme/projects`, () => HttpResponse.json([frontend])),
  http.get(`${API_URL}/orgs/acme/members`, () =>
    HttpResponse.json([aliceMembership]),
  ),
];

/** Everything the backend guards answers 401 without a session. */
export function unauthenticated() {
  return http.get(`${API_URL}/*`, () =>
    HttpResponse.json({ kind: "unauthenticated", message: "no session" }, {
      status: 401,
    }),
  );
}
