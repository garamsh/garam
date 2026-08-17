import {
  GaramUnreachableError,
  garamFetch,
  isUnauthenticated,
} from "@/lib/garam";
import type { components } from "@/types/api";

/** The signed-in user, as the backend describes one. */
export type User = components["schemas"]["User"];

/**
 * What the backend was able to say about the caller's session. An API
 * that never answered is its own answer: a caller that collapsed it into
 * "signed out" would send a visitor to a login page that cannot work.
 */
export type SessionState =
  | { status: "signed-in"; user: User }
  | { status: "signed-out" }
  | { status: "unreachable" };

/**
 * Asks the backend who the caller is. The backend's `/me` is the only
 * source of session truth; the cookie alone proves nothing (it may be
 * expired or revoked).
 */
export async function getSession(): Promise<SessionState> {
  try {
    return { status: "signed-in", user: await garamFetch<User>("/me") };
  } catch (error) {
    if (isUnauthenticated(error)) {
      return { status: "signed-out" };
    }
    if (error instanceof GaramUnreachableError) {
      return { status: "unreachable" };
    }
    throw error;
  }
}
