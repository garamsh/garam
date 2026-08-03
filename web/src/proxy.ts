import { NextResponse, type NextRequest } from "next/server";

import { SESSION_COOKIE } from "@/lib/session";

/**
 * Sends visitors without a session cookie to the login page before the
 * console shell renders. This is a fast path, not the authorization
 * gate: a cookie may be expired or revoked, so `/console` verifies the
 * session against the backend's `/me`, and the login page sends a
 * visitor who already has a valid one on to the console.
 */
export function proxy(request: NextRequest) {
  if (request.cookies.has(SESSION_COOKIE)) {
    return NextResponse.next();
  }
  return NextResponse.redirect(new URL("/login", request.url));
}

export const config = {
  // Everything outside the console — the landing page, `/login`, static
  // assets, and the `/api` rewrite the OAuth round trip runs through —
  // is public.
  matcher: "/console/:path*",
};
