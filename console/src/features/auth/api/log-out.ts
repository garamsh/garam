"use server";

import { redirect } from "next/navigation";

import { garamRequest, isUnauthenticated } from "@/lib/garam";
import { clearSessionCookie } from "@garam/platform-client/session-cookie";

/**
 * Revokes the session server-side and clears the cookie. A session the
 * backend already rejects still logs the browser out, so a 401 here is
 * success rather than an error. Any other failure propagates, but the
 * cookie goes with the attempt: the user asked to be signed out, and
 * server-side revocation is best-effort.
 */
export async function logOut() {
  let response: Response | undefined;
  try {
    response = await garamRequest("/auth/logout", { method: "POST" });
  } catch (error) {
    if (!isUnauthenticated(error)) throw error;
  } finally {
    // The backend's Set-Cookie does not reach the browser through a
    // Server Action response, so it is replayed here — in the finally,
    // so a failed revocation cannot leave the cookie behind.
    await clearSessionCookie(response);
  }
  redirect("/login");
}
