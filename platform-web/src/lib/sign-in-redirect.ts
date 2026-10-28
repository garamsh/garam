import "server-only";

import { headers } from "next/headers";
import { redirect } from "next/navigation";

import {
  RETURN_PATH_HEADER,
  safeReturnPath,
  withReturnPath,
} from "@garam/platform-sdk-ts/return-path";

import { HANDOFF_START_PATH } from "@garam/platform-sdk-ts/handoff";

/**
 * Sends a visitor whose session the backend rejected to sign in again,
 * by the handoff from the site (ADR-0131), carrying the console path the
 * request was for, so an invitation opened with an expired cookie keeps
 * its token through the sign-in that follows. The handoff's redemption
 * replaces the cookie the backend rejected.
 */
export async function redirectToSignIn(): Promise<never> {
  const path = safeReturnPath((await headers()).get(RETURN_PATH_HEADER));
  redirect(withReturnPath(HANDOFF_START_PATH, path));
}
