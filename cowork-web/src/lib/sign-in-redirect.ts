import "server-only";

import { headers } from "next/headers";
import { redirect } from "next/navigation";

import { HANDOFF_START_PATH } from "@garam/platform-sdk-ts/handoff";
import {
  RETURN_PATH_HEADER,
  safeReturnPath,
  withReturnPath,
} from "@garam/platform-sdk-ts/return-path";

/**
 * Sends a visitor whose session the platform refused to sign in again,
 * by the handoff from the site (ADR-0131), carrying the path the
 * request was for. This is how a sign-out on any host reaches this one:
 * the handoff's redemption replaces the cookie the platform refused.
 */
export async function redirectToSignIn(): Promise<never> {
  const path = safeReturnPath(
    (await headers()).get(RETURN_PATH_HEADER),
    "cowork",
  );
  redirect(withReturnPath(HANDOFF_START_PATH, path));
}
