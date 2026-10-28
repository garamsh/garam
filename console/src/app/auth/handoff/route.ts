import { cookies } from "next/headers";
import { NextResponse, type NextRequest } from "next/server";

import {
  CODE_PARAM,
  HANDOFF_REDEEM_PATH,
  HANDOFF_STATE_COOKIE,
  handoffCodeOf,
  handoffStateOf,
} from "@garam/platform-client/handoff";
import {
  RETURN_PARAM,
  safeReturnPath,
  withReturnPath,
} from "@garam/platform-client/return-path";
import { keepSessionCookie } from "@garam/platform-client/session-cookie";

import { GaramApiError, garamRequest } from "@/lib/garam";
import { siteUrl } from "@/lib/site-url";

/**
 * Redeems the code the site handed back (ADR-0131) with the state this
 * host drew when it sent the visitor there, and plants the session the
 * backend issued for this host. The state cookie is spent either way:
 * a state redeems one handoff.
 *
 * A code that does not redeem — lapsed, spent, or meeting a state this
 * browser does not hold — sends the visitor to sign in on the site,
 * which hands a session that does exist straight back.
 */
export async function GET(request: NextRequest) {
  const params = request.nextUrl.searchParams;
  const next = safeReturnPath(params.get(RETURN_PARAM)) ?? "/console";
  const code = handoffCodeOf(params.get(CODE_PARAM));
  const state = handoffStateOf(
    request.cookies.get(HANDOFF_STATE_COOKIE)?.value,
  );
  (await cookies()).delete({
    name: HANDOFF_STATE_COOKIE,
    path: HANDOFF_REDEEM_PATH,
  });

  let destination = siteUrl(withReturnPath("/login", next));
  if (code !== null && state !== null) {
    try {
      const response = await garamRequest("/auth/handoff/redeem", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ code, state }),
      });
      await keepSessionCookie(response);
      destination = next;
    } catch (error) {
      if (!refused(error)) throw error;
    }
  }
  // The code rides in this route's address, and the return path can
  // hold an invitation token. The console path is relative: it is this
  // host's, whatever name the request reached it by.
  return new NextResponse(null, {
    status: 303,
    headers: { location: destination, "referrer-policy": "no-referrer" },
  });
}

/** Whether the backend refused the redemption rather than failing. */
function refused(error: unknown): boolean {
  return (
    error instanceof GaramApiError &&
    (error.status === 400 || error.status === 401)
  );
}
