import { NextResponse, type NextRequest } from "next/server";

import {
  RETURN_PATH_HEADER,
  withReturnPath,
} from "@garam/platform-client/return-path";
import { SESSION_COOKIE } from "@garam/platform-client/session";

import { env } from "@/env";

/**
 * Sends visitors without a session cookie to the site's login page
 * before the console shell renders (ADR-0131). This is a fast path, not
 * the authorization gate: a cookie may be expired or revoked, so
 * `/console` verifies the session against the backend's `/me`, and the
 * login page sends a visitor who already has a valid one back here.
 *
 * The address asked for travels to the login page as its return path,
 * so signing in — or registering from an invitation — lands where the
 * visitor was going rather than on the console's front page.
 *
 * `/login` and `/register` were this app's until the site took them,
 * so a bookmark of either is sent on with its query, and this host's
 * root opens the console.
 */
export function proxy(request: NextRequest) {
  const { pathname, search } = request.nextUrl;
  if (pathname === "/") {
    return NextResponse.redirect(new URL("/console", request.url));
  }
  if (pathname === "/login" || pathname === "/register") {
    return NextResponse.redirect(
      new URL(pathname + search, env.GARAM_SITE_URL),
    );
  }
  if (request.cookies.has(SESSION_COOKIE)) {
    // A cookie the backend no longer honours is found out further in,
    // where the URL is not at hand; the path travels with the request.
    const headers = new Headers(request.headers);
    headers.set(RETURN_PATH_HEADER, pathname + search);
    return NextResponse.next({ request: { headers } });
  }
  return NextResponse.redirect(
    new URL(withReturnPath("/login", pathname + search), env.GARAM_SITE_URL),
  );
}

/** The routes `proxy` runs on; Next.js reads this export by name. */
export const config = {
  // Everything else — static assets, the device page's forward, and
  // the `/api` rewrite the OAuth round trip runs through — is public.
  matcher: ["/", "/login", "/register", "/console/:path*"],
};
