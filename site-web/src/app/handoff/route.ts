import { NextResponse, type NextRequest } from "next/server";
import { z } from "zod";

import {
  APP_PARAM,
  CODE_PARAM,
  HANDOFF_REDEEM_PATH,
  HANDOFF_START_PATH,
  STATE_PARAM,
  handoffAppOf,
  handoffStateOf,
} from "@garam/platform-sdk-ts/handoff";
import {
  DEFAULT_RETURN_PATH,
  RETURN_PARAM,
  safeReturnPath,
  withReturnPath,
} from "@garam/platform-sdk-ts/return-path";
import { SESSION_COOKIE } from "@garam/platform-sdk-ts/session";

import { appUrl, withApp } from "@/lib/app-url";
import { garamFetch, isUnauthenticated, parserFor } from "@/lib/garam";
import type { components } from "@garam/platform-sdk-ts/api";

const HandoffParser = parserFor<components["schemas"]["SessionHandoff"]>()(
  z.object({ code: z.string() }),
);

/**
 * Hands this site's sign-in to another of garam's apps (ADR-0131,
 * ADR-0135). The session cookie is host-only, so the app sends a
 * visitor here naming itself and a state it holds, and a visitor signed
 * in here goes back with a one-time code that redeems only with that
 * state. A visitor not signed in here signs in first, and the sign-in
 * brings them back through the app, which starts the handoff again.
 *
 * The app is named by identifier and its origin is this site's
 * configuration, so no request decides where a code goes: one naming
 * an app not on the list is refused. A request carrying no state the
 * app drew did not come from it, so the app is asked to start over
 * rather than handed a code nothing can redeem.
 */
export async function GET(request: NextRequest) {
  const params = request.nextUrl.searchParams;
  const app = handoffAppOf(params.get(APP_PARAM));
  if (app === null) {
    return new NextResponse("No such app to sign in to.", { status: 400 });
  }
  const next =
    safeReturnPath(params.get(RETURN_PARAM), app) ?? DEFAULT_RETURN_PATH[app];
  const state = handoffStateOf(params.get(STATE_PARAM));
  if (state === null) {
    return redirect(appUrl(app, withReturnPath(HANDOFF_START_PATH, next)));
  }
  if (!request.cookies.has(SESSION_COOKIE)) {
    return redirect(withApp("/login", app, next));
  }
  let code: string;
  try {
    ({ code } = await garamFetch("/auth/handoff", HandoffParser, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ state }),
    }));
  } catch (error) {
    if (isUnauthenticated(error)) {
      return redirect(withApp("/login", app, next));
    }
    throw error;
  }
  const target = new URL(appUrl(app, HANDOFF_REDEEM_PATH));
  target.searchParams.set(CODE_PARAM, code);
  target.searchParams.set(RETURN_PARAM, next);
  return redirect(target.toString());
}

/**
 * A redirect that sends no referrer: the code rides in the app's
 * address, and the return path can hold an invitation token. A path is
 * this host's, whatever name the request reached it by.
 */
function redirect(location: string): NextResponse {
  return new NextResponse(null, {
    status: 303,
    headers: { location, "referrer-policy": "no-referrer" },
  });
}
