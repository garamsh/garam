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
 * It also answers `/api/*` from the backend at `GARAM_API_URL`, with
 * the `/api` segment dropped (ADR-0009), as the console does.
 *
 * It runs when the request arrives, so the console's and the
 * backend's addresses are the deployment's and not the build's.
 */
export function proxy(request: NextRequest) {
  const { pathname, search } = request.nextUrl;
  if (pathname === "/api" || pathname.startsWith("/api/")) {
    const path = pathname.replace(/^\/api\/?/, "");
    return NextResponse.rewrite(
      new URL(`${env.GARAM_API_URL}/${path}${search}`),
    );
  }
  return NextResponse.redirect(
    new URL(pathname + search, env.GARAM_CONSOLE_URL),
  );
}

/** The routes `proxy` runs on; Next.js reads this export by name. */
export const config = {
  matcher: ["/console/:path*", "/device", "/api/:path*"],
};
