import { me } from "@/lib/platform";

/**
 * Whether the caller's session stands: 204, or 401 when the platform
 * does not accept it. A WebSocket upgrade's refusal is unreadable in a
 * browser, so a tab whose socket failed asks here instead (ADR-0139).
 * A platform failure is a 5xx, which says nothing of the session. It
 * is a route and not an action because the proxy sends a request with
 * no cookie to sign in, and that redirect would hide the very refusal
 * asked about; `auth/` is the part it leaves alone.
 */
export async function GET() {
  return new Response(null, { status: (await me()) ? 204 : 401 });
}
