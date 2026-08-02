"use server";

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { garamFetch, isUnauthenticated } from "@/lib/garam";
import { SESSION_COOKIE } from "@/lib/session";

/**
 * Revokes the session server-side and clears the cookie. A session the
 * backend already rejects still logs the browser out, so a 401 here is
 * success rather than an error.
 */
export async function logOut() {
  try {
    await garamFetch("/auth/logout", { method: "POST" });
  } catch (error) {
    if (!isUnauthenticated(error)) throw error;
  }
  // The backend's Set-Cookie does not reach the browser through a
  // Server Action response, so the cookie is cleared here as well.
  (await cookies()).delete(SESSION_COOKIE);
  redirect("/login");
}
