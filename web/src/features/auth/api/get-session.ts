import { garamFetch, isUnauthenticated } from "@/lib/garam";
import type { components } from "@/types/api";

/** The signed-in user, as the backend describes one. */
export type User = components["schemas"]["User"];

/**
 * Returns the signed-in user, or null when there is no valid session.
 * The backend's `/me` is the only source of session truth; the cookie
 * alone proves nothing (it may be expired or revoked).
 */
export async function getSession(): Promise<User | null> {
  try {
    return await garamFetch<User>("/me");
  } catch (error) {
    if (isUnauthenticated(error)) return null;
    throw error;
  }
}
