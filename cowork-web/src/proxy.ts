import { NextResponse, type NextRequest } from "next/server";

import { HANDOFF_START_PATH } from "@garam/platform-sdk-ts/handoff";
import {
  RETURN_PATH_HEADER,
  withReturnPath,
} from "@garam/platform-sdk-ts/return-path";
import { SESSION_COOKIE } from "@garam/platform-sdk-ts/session";

/**
 * Sends visitors without a session cookie to sign in before a page
 * renders, by the handoff from the site `HANDOFF_START_PATH` starts
 * (ADR-0131). This is a fast path, not the authorization gate: a cookie
 * may be expired or revoked, so a page that finds the platform refusing
 * it starts the same handoff, and the path travels with the request for
 * that.
 */
export function proxy(request: NextRequest) {
  const { pathname, search } = request.nextUrl;
  if (request.cookies.has(SESSION_COOKIE)) {
    const headers = new Headers(request.headers);
    headers.set(RETURN_PATH_HEADER, pathname + search);
    return NextResponse.next({ request: { headers } });
  }
  return NextResponse.redirect(
    new URL(withReturnPath(HANDOFF_START_PATH, pathname + search), request.url),
  );
}

/** The routes `proxy` runs on; Next.js reads this export by name. */
export const config = {
  // Everything but the handoff's own routes, the probes and static
  // assets, which a visitor without a session has to reach.
  matcher: ["/((?!auth/|healthz$|readyz$|_next/|favicon\\.ico).*)"],
};
