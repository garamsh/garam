import { HttpResponse, http, type JsonBodyType } from "msw";

import type { components } from "@garam/platform-sdk-ts/api";
import type { User } from "@/features/auth";
import type { InvitationPreview } from "@/features/invitations";

/** The backend base URL the tests run the server-side client against. */
export const API_URL = "http://localhost:8080";

/** The signed-in user every test runs as. */
export const alice: User = {
  grn: "grn:root:default:user:alice",
  nickname: "Alice",
};

/**
 * Alice's membership of Acme, which a registration from an invitation
 * answers with. `nickname` is her user row's.
 */
export const aliceMembership: components["schemas"]["Member"] = {
  userGrn: alice.grn,
  nickname: alice.nickname,
  role: "owner",
  since: "2026-01-02T03:04:05Z",
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

/**
 * The routes the site reads. The console's fixtures model the rest of
 * the API; the site reaches only who is signed in, the ways in, and
 * the invitation a sign-in was started for.
 */
const routes: { path: string; guarded: boolean; body: JsonBodyType }[] = [
  { path: "/me", guarded: true, body: alice },
  { path: "/auth/providers", guarded: false, body: ["google"] },
  {
    path: "/invitations/secret-token",
    guarded: false,
    body: acmeInvitation,
  },
];

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
