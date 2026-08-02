import { NextResponse, type NextRequest } from "next/server";

import { SESSION_COOKIE } from "@/lib/session";

/**
 * Sends visitors without a session cookie to the login page before the
 * app shell renders. This is a fast path, not the authorization gate:
 * a cookie may be expired or revoked, so every authenticated segment
 * still verifies the session against the backend's `/me`.
 */
export function proxy(request: NextRequest) {
  if (request.cookies.has(SESSION_COOKIE)) {
    return NextResponse.next();
  }
  const login = new URL("/login", request.url);
  return NextResponse.redirect(login);
}

export const config = {
  // `/login` and the backend proxy must stay reachable while signed
  // out — the OAuth round trip runs through `/api/auth/*`.
  matcher: "/((?!api|login|_next/static|_next/image|favicon.ico).*)",
};
