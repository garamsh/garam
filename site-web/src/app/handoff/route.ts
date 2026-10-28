import { NextResponse, type NextRequest } from "next/server";
import { z } from "zod";

import {
  CODE_PARAM,
  HANDOFF_REDEEM_PATH,
  HANDOFF_START_PATH,
  STATE_PARAM,
  handoffStateOf,
} from "@garam/platform-sdk-ts/handoff";
import {
  RETURN_PARAM,
  safeReturnPath,
  withReturnPath,
} from "@garam/platform-sdk-ts/return-path";
import { SESSION_COOKIE } from "@garam/platform-sdk-ts/session";

import { consoleUrl } from "@/lib/console-url";
import { garamFetch, isUnauthenticated, parserFor } from "@/lib/garam";
import type { components } from "@garam/platform-sdk-ts/api";

const HandoffParser = parserFor<components["schemas"]["SessionHandoff"]>()(
  z.object({ code: z.string() }),
);

/**
 * Hands this site's sign-in to the console (ADR-0131). The session
 * cookie is host-only, so the console sends a visitor here with a state
 * it holds, and a visitor signed in here goes back with a one-time code
 * that redeems only with that state. A visitor not signed in here signs
 * in first; the console path they were going to brings them back through
 * the console, which starts the handoff again.
 *
 * A request carrying no state the console drew did not come from the
 * console, so the console is asked to start over rather than handed a
 * code nothing can redeem.
 */
export async function GET(request: NextRequest) {
  const params = request.nextUrl.searchParams;
  const next = safeReturnPath(params.get(RETURN_PARAM)) ?? "/console";
  const state = handoffStateOf(params.get(STATE_PARAM));
  if (state === null) {
    return redirect(consoleUrl(withReturnPath(HANDOFF_START_PATH, next)));
  }
  if (!request.cookies.has(SESSION_COOKIE)) {
    return redirect(withReturnPath("/login", next));
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
      return redirect(withReturnPath("/login", next));
    }
    throw error;
  }
  const target = new URL(consoleUrl(HANDOFF_REDEEM_PATH));
  target.searchParams.set(CODE_PARAM, code);
  target.searchParams.set(RETURN_PARAM, next);
  return redirect(target.toString());
}

/**
 * A redirect that sends no referrer: the code rides in the console's
 * address, and the return path can hold an invitation token. A path is
 * this host's, whatever name the request reached it by.
 */
function redirect(location: string): NextResponse {
  return new NextResponse(null, {
    status: 303,
    headers: { location, "referrer-policy": "no-referrer" },
  });
}
