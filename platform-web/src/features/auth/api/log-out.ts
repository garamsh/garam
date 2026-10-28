"use server";

import { redirect } from "next/navigation";

import { garamRequest, isUnauthenticated } from "@/lib/garam";
import { siteUrl } from "@/lib/site-url";
import { clearSessionCookie } from "@garam/platform-sdk-ts/session-cookie";

/**
 * Revokes the session server-side and clears the cookie. The backend
 * ends the sign-in this session was handed off from with it (ADR-0131),
 * so the site the visitor is sent to is signed out too. A session the
 * backend already rejects still logs the browser out, so a 401 here is
 * success rather than an error. Any other failure propagates, but the
 * cookie goes with the attempt: the user asked to be signed out, and
 * server-side revocation is best-effort.
 */
export async function logOut() {
  try {
    await garamRequest("/auth/logout", { method: "POST" });
  } catch (error) {
    if (!isUnauthenticated(error)) throw error;
  } finally {
    // The backend's Set-Cookie does not reach the browser through a
    // Server Action response, so it is replayed here — in the finally,
    // so a failed revocation cannot leave the cookie behind.
    await clearSessionCookie();
  }
  redirect(siteUrl("/login"));
}
