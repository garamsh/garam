import { z } from "zod";

import {
  GaramUnreachableError,
  garamFetch,
  isUnauthenticated,
  parserFor,
} from "@/lib/garam";
import type { components } from "@/types/api";

/** The signed-in user, as the backend describes one. */
export type User = components["schemas"]["User"];

// Tied to the type `api/openapi.yaml` generated, in both directions: a
// field the specification adds, drops or retypes fails the typecheck
// here rather than reaching the shell as a cast, and a field only the
// parser demands fails it rather than refusing a valid response as a
// contract violation.
const UserParser = parserFor<User>()(
  z.object({
    grn: z.string(),
    nickname: z.string(),
  }),
);

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
    return { status: "signed-in", user: await garamFetch("/me", UserParser) };
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
