import { NextResponse, type NextRequest } from "next/server";

import {
  RETURN_PATH_HEADER,
  withReturnPath,
} from "@garam/platform-sdk-ts/return-path";
import { HANDOFF_START_PATH } from "@garam/platform-sdk-ts/handoff";
import { SESSION_COOKIE } from "@garam/platform-sdk-ts/session";

import { env } from "@/env";

/**
 * Sends visitors without a session cookie to sign in before the console
 * shell renders. The session cookie is host-only (ADR-0131), so signing
 * in is a handoff from the site, which `HANDOFF_START_PATH` starts. This
 * is a fast path, not the authorization gate: a cookie may be expired or
 * revoked, so `/console` verifies the session against the backend's
 * `/me` and starts the same handoff when it is refused.
 *
 * The address asked for travels through the handoff as its return path,
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
    new URL(withReturnPath(HANDOFF_START_PATH, pathname + search), request.url),
  );
}

/** The routes `proxy` runs on; Next.js reads this export by name. */
export const config = {
  // Everything else — static assets, the device page's forward, and
  // the `/api` rewrite the OAuth round trip runs through — is public.
  matcher: ["/", "/login", "/register", "/console/:path*"],
};
