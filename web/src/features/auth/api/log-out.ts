"use server";

import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import { z } from "zod";

import { garamFetch, isUnauthenticated } from "@/lib/garam";
import { SESSION_COOKIE } from "@/lib/session";

/**
 * Revokes the session server-side and clears the cookie. A session the
 * backend already rejects still logs the browser out, so a 401 here is
 * success rather than an error. Any other failure propagates, but the
 * cookie goes with the attempt: the user asked to be signed out, and
 * server-side revocation is best-effort.
 */
export async function logOut() {
  try {
    await garamFetch("/auth/logout", z.unknown(), { method: "POST" });
  } catch (error) {
    if (!isUnauthenticated(error)) throw error;
  } finally {
    // The backend's Set-Cookie does not reach the browser through a
    // Server Action response, so the cookie is cleared here as well —
    // in the finally, so a failed revocation cannot leave it behind.
    (await cookies()).delete(SESSION_COOKIE);
  }
  redirect("/login");
}
