import { NextResponse, type NextRequest } from "next/server";

import {
  APP_PARAM,
  HANDOFF_PATH,
  HANDOFF_REDEEM_PATH,
  HANDOFF_STATE_COOKIE,
  HANDOFF_STATE_MAX_AGE,
  STATE_PARAM,
  newHandoffState,
} from "@garam/platform-sdk-ts/handoff";
import {
  DEFAULT_RETURN_PATH,
  RETURN_PARAM,
  safeReturnPath,
} from "@garam/platform-sdk-ts/return-path";

import { env } from "@/env";

/**
 * Starts a sign-in handoff (ADR-0131, ADR-0135). The session cookie is
 * host-only, so a person signs in on the site and this host is handed a
 * session of its own: a state is drawn and held here in a cookie only
 * the redemption route is sent, and the visitor goes to the site naming
 * this app, with it. A code obtained anywhere else meets a browser that
 * never held this state.
 *
 * A browser navigation lands here, from the proxy or from a page whose
 * session the platform refused, so this is a route and not an action.
 */
export function GET(request: NextRequest) {
  const next =
    safeReturnPath(request.nextUrl.searchParams.get(RETURN_PARAM), "cowork") ??
    DEFAULT_RETURN_PATH.cowork;
  const state = newHandoffState();
  const target = new URL(HANDOFF_PATH, env.GARAM_SITE_URL);
  target.searchParams.set(APP_PARAM, "cowork");
  target.searchParams.set(RETURN_PARAM, next);
  target.searchParams.set(STATE_PARAM, state);
  const response = NextResponse.redirect(target);
  response.cookies.set({
    name: HANDOFF_STATE_COOKIE,
    value: state,
    path: HANDOFF_REDEEM_PATH,
    maxAge: HANDOFF_STATE_MAX_AGE,
    httpOnly: true,
    sameSite: "lax",
    secure: servedOverTls(request),
  });
  response.headers.set("referrer-policy", "no-referrer");
  return response;
}

/**
 * Whether the browser reached this host over TLS. The gateway in front
 * of a deployment terminates it and says so in `x-forwarded-proto`; the
 * local stack serves plain HTTP, where a `Secure` cookie would never
 * come back.
 */
function servedOverTls(request: NextRequest): boolean {
  const forwarded = request.headers.get("x-forwarded-proto");
  return (forwarded ?? request.nextUrl.protocol.replace(":", "")) === "https";
}
