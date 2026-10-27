import "server-only";

import { cookies } from "next/headers";

import { env } from "@/env";
import { SESSION_COOKIE } from "@/lib/cowork";

/** An organization the caller belongs to, as cowork names it. */
export type Organization = { slug: string; name: string };

/**
 * The organizations the caller belongs to, asked of the platform with
 * the caller's own cookie, or `null` when the platform does not accept
 * it and the caller has to sign in.
 */
export async function listOrganizations(): Promise<Organization[] | null> {
  const session = (await cookies()).get(SESSION_COOKIE)?.value;
  if (!session) return null;
  const response = await fetch(`${env.GARAM_API_URL}/orgs`, {
    cache: "no-store",
    headers: { Cookie: `${SESSION_COOKIE}=${session}` },
  });
  if (response.status === 401) return null;
  if (!response.ok)
    throw new Error(`platform: listOrgs answered ${response.status}`);
  const orgs = (await response.json()) as { grn: string; name: string }[];
  return orgs.map((o) => ({
    slug: o.grn.split(":").at(-1) ?? o.grn,
    name: o.name,
  }));
}

/** Who the caller is, as the platform says, or `null` when signed out. */
export async function me(): Promise<{ grn: string; nickname: string } | null> {
  const session = (await cookies()).get(SESSION_COOKIE)?.value;
  if (!session) return null;
  const response = await fetch(`${env.GARAM_API_URL}/me`, {
    cache: "no-store",
    headers: { Cookie: `${SESSION_COOKIE}=${session}` },
  });
  if (response.status === 401) return null;
  if (!response.ok) throw new Error(`platform: me answered ${response.status}`);
  return (await response.json()) as { grn: string; nickname: string };
}
