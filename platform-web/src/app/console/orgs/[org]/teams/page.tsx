import type { Metadata } from "next";
import Link from "next/link";

import { Button } from "@garam/design/ui/button";
import { getOrg, lookupGroups, slugFromGrn } from "@/features/orgs";
import { checkPermissions } from "@/lib/permission-check";

import { ApiUnreachable } from "../../../_components/api-unreachable";
import { NEW_ITEM, orgHref } from "../../../_components/navigation";
import { RefusedRead } from "../../../_components/refused-read";
import { ListPage } from "../../../_components/templates/list-page";
import { TeamTable } from "./_components/team-table";

/** Head metadata for the teams route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Teams — garam" };

type Params = { params: Promise<{ org: string }> };

const TITLE = "Teams";
/**
 * The organization's teams, each opening a page of its own. A team is
 * how the console presents a group (ADR-0072): the API, its GRN and its
 * permissions are the group's.
 *
 * The organization is read for itself, so one the caller cannot read
 * answers 404 (ADR-0011); inside one it can read, a refused list says
 * so rather than showing none. The page asks the server whether the
 * caller may create a team and leaves the action out only when the
 * answer is no.
 */
export default async function TeamsPage({ params }: Params) {
  const { org } = await params;
  const [organization, teams, permissions] = await Promise.all([
    getOrg(org),
    lookupGroups(org),
    checkPermissions(org, { create: { action: "group:create" } }),
  ]);

  const create = permissions.get("create") !== "denied" && (
    <Button asChild>
      <Link href={`${orgHref(org, "teams")}/${NEW_ITEM}`}>Create team</Link>
    </Button>
  );

  if (organization.status === "unreachable" || teams.status === "unreachable") {
    return (
      <ListPage title={TITLE}>
        <ApiUnreachable subject="this organization's teams" />
      </ListPage>
    );
  }
  if (teams.status === "forbidden") {
    return (
      <ListPage title={TITLE} primaryAction={create}>
        <RefusedRead subject="this organization's teams" />
      </ListPage>
    );
  }

  return (
    <ListPage title={TITLE} count={teams.data.length} primaryAction={create}>
      <TeamTable
        org={org}
        emptyAction={create}
        teams={teams.data.map((team) => ({
          name: team.name,
          identifier: slugFromGrn(team.grn),
          roles: team.roles,
        }))}
      />
    </ListPage>
  );
}
