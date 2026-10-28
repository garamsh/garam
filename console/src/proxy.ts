import { NextResponse, type NextRequest } from "next/server";

import { RETURN_PATH_HEADER, withReturnPath } from "@garam/platform-client/return-path";
import { SESSION_COOKIE } from "@garam/platform-client/session";

/**
 * Sends visitors without a session cookie to the login page before the
 * console shell renders. This is a fast path, not the authorization
 * gate: a cookie may be expired or revoked, so `/console` verifies the
 * session against the backend's `/me`, and the login page sends a
 * visitor who already has a valid one on to the console.
 *
 * The address asked for travels to the login page as its return path,
 * so signing in — or registering from an invitation — lands where the
 * visitor was going rather than on the console's front page.
 */
export function proxy(request: NextRequest) {
  const { pathname, search } = request.nextUrl;
  if (request.cookies.has(SESSION_COOKIE)) {
    // A cookie the backend no longer honours is found out further in,
    // where the URL is not at hand; the path travels with the request.
    const headers = new Headers(request.headers);
    headers.set(RETURN_PATH_HEADER, pathname + search);
    return NextResponse.next({ request: { headers } });
  }
  return NextResponse.redirect(
    new URL(withReturnPath("/login", pathname + search), request.url),
  );
}

/** The routes `proxy` runs on; Next.js reads this export by name. */
export const config = {
  // Everything outside the console — the landing page, `/login`, static
  // assets, and the `/api` rewrite the OAuth round trip runs through —
  // is public.
  matcher: "/console/:path*",
};
