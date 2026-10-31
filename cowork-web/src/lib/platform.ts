import "server-only";

import { cookies } from "next/headers";

import { SESSION_COOKIE } from "@garam/platform-sdk-ts/session";

import { env } from "@/env";

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

/** Someone a message can mention: a person or an agent of the organization. */
export type Participant = {
  grn: string;
  name: string;
  kind: "person" | "agent";
};

/**
 * The organization's people and agents, as the platform lists them to
 * the caller. A list the caller may not read is left empty rather than
 * failing the page: mentioning is a convenience.
 */
export async function listParticipants(org: string): Promise<Participant[]> {
  const session = (await cookies()).get(SESSION_COOKIE)?.value;
  if (!session) return [];
  const get = async <T>(path: string): Promise<T[]> => {
    const response = await fetch(
      `${env.GARAM_API_URL}/orgs/${encodeURIComponent(org)}${path}`,
      {
        cache: "no-store",
        headers: { Cookie: `${SESSION_COOKIE}=${session}` },
      },
    );
    return response.ok ? ((await response.json()) as T[]) : [];
  };
  const [members, agents] = await Promise.all([
    get<{ userGrn: string; nickname?: string }>("/members"),
    get<{ grn: string; name?: string }>("/agents"),
  ]);
  return [
    ...members.map((m) => ({
      grn: m.userGrn,
      name: m.nickname ?? m.userGrn.split(":").at(-1) ?? m.userGrn,
      kind: "person" as const,
    })),
    ...agents.map((a) => ({
      grn: a.grn,
      name: a.name ?? a.grn.split(":").at(-1) ?? a.grn,
      kind: "agent" as const,
    })),
  ];
}
