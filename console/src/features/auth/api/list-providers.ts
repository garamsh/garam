import { z } from "zod";

import { garamFetch } from "@/lib/garam";

/**
 * The sign-in providers the backend has registered. The login page
 * renders a button per name it holds one for, so a provider the backend
 * does not serve is never offered (ADR-0010).
 *
 * A read that fails yields no names rather than a rejection, and the
 * two are deliberately not distinguished the way `OrgRead` distinguishes
 * them: both mean the page cannot confirm a door, and offering one it
 * cannot confirm is the failure this endpoint exists to prevent. The
 * password form is unaffected, so a visitor still has a way in.
 */
export async function listProviders(): Promise<string[]> {
  try {
    return await garamFetch("/auth/providers", z.array(z.string()));
  } catch {
    return [];
  }
}
