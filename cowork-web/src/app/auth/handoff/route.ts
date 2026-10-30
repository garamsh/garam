import { cookies } from "next/headers";
import { NextResponse, type NextRequest } from "next/server";

import {
  APP_PARAM,
  CODE_PARAM,
  HANDOFF_REDEEM_PATH,
  HANDOFF_STATE_COOKIE,
  handoffCodeOf,
  handoffStateOf,
} from "@garam/platform-sdk-ts/handoff";
import {
  DEFAULT_RETURN_PATH,
  RETURN_PARAM,
  safeReturnPath,
  withReturnPath,
} from "@garam/platform-sdk-ts/return-path";
import { keepSessionCookie } from "@garam/platform-sdk-ts/session-cookie";

import { env } from "@/env";

/**
 * Redeems the code the site handed back (ADR-0131) with the state this
 * host drew when it sent the visitor there, and plants the session the
 * platform issued for this host. The state cookie is spent either way:
 * a state redeems one handoff.
 *
 * A code that does not redeem — lapsed, spent, or meeting a state this
 * browser does not hold — sends the visitor to sign in on the site,
 * which hands a session that does exist straight back.
 */
export async function GET(request: NextRequest) {
  const params = request.nextUrl.searchParams;
  const next =
    safeReturnPath(params.get(RETURN_PARAM), "cowork") ??
    DEFAULT_RETURN_PATH.cowork;
  const code = handoffCodeOf(params.get(CODE_PARAM));
  const state = handoffStateOf(
    request.cookies.get(HANDOFF_STATE_COOKIE)?.value,
  );
  (await cookies()).delete({
    name: HANDOFF_STATE_COOKIE,
    path: HANDOFF_REDEEM_PATH,
  });

  let destination = signInOnTheSite(next);
  if (code !== null && state !== null) {
    const response = await fetch(`${env.GARAM_API_URL}/auth/handoff/redeem`, {
      method: "POST",
      cache: "no-store",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ code, state }),
    });
    if (response.ok) {
      await keepSessionCookie(response);
      destination = next;
    } else if (response.status !== 400 && response.status !== 401) {
      throw new Error(`platform: redeem answered ${response.status}`);
    }
  }
  // The code rides in this route's address. The path is relative: it is
  // this host's, whatever name the request reached it by.
  return new NextResponse(null, {
    status: 303,
    headers: { location: destination, "referrer-policy": "no-referrer" },
  });
}

/** The site's sign-in, naming this app so it returns here. */
function signInOnTheSite(next: string): string {
  const target = new URL(withReturnPath("/login", next), env.GARAM_SITE_URL);
  target.searchParams.set(APP_PARAM, "cowork");
  return target.toString();
}
