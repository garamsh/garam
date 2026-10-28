import { NextResponse, type NextRequest } from "next/server";

import { env } from "@/env";

/**
 * Sends every address the console answers to the console's own origin
 * (ADR-0131), keeping the path and the query. The console was served
 * under `/console` on this host, so a bookmark of it, a return path
 * the backend's OAuth callback lands on, and `/device`, which
 * `garam login` names relative to this host's `/api`, all arrive here
 * and are sent on rather than answered.
 *
 * It runs when the request arrives, so the console's address is the
 * deployment's and not the build's.
 */
export function proxy(request: NextRequest) {
  const { pathname, search } = request.nextUrl;
  return NextResponse.redirect(
    new URL(pathname + search, env.GARAM_CONSOLE_URL),
  );
}

/** The routes `proxy` runs on; Next.js reads this export by name. */
export const config = {
  matcher: ["/console/:path*", "/device"],
};
