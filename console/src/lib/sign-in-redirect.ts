import "server-only";

import { headers } from "next/headers";
import { redirect } from "next/navigation";

import {
  RETURN_PATH_HEADER,
  safeReturnPath,
  withReturnPath,
} from "@garam/platform-client/return-path";

import { siteUrl } from "@/lib/site-url";

/**
 * Sends a visitor whose session the backend rejected to sign in on the
 * site, carrying the console path the request was for, so an invitation
 * opened with an expired cookie keeps its token through the sign-in that
 * follows.
 */
export async function redirectToSignIn(): Promise<never> {
  const path = safeReturnPath((await headers()).get(RETURN_PATH_HEADER));
  redirect(siteUrl(withReturnPath("/login", path)));
}
