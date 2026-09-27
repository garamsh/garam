import type { Metadata } from "next";
import Link from "next/link";
import { UsersRound } from "lucide-react";

import { Button } from "@/components/ui/button";
import { listGroups } from "@/features/orgs";

import { ApiUnreachable } from "../../../_components/api-unreachable";
import { EmptyState } from "../../../_components/empty-state";
import { NEW_ITEM, orgHref } from "../../../_components/navigation";
import { PageHeader } from "../../../_components/page-header";
import { TeamList } from "./_components/team-list";

/** Head metadata for the teams route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Teams — garam" };

type Params = { params: Promise<{ org: string }> };

/**
 * The organization's teams, each opening a page of its own. A team is
 * how the console presents a group (ADR-0072): the API, its GRN and its
 * permissions are the group's.
 *
 * Creating is granted by the `admin` role while every member reads the
 * list. The create action is offered to all of them and the API
 * refuses a caller that holds only the read — the same shape the
 * operators section takes.
 */
export default async function TeamsPage({ params }: Params) {
  const { org } = await params;
  const teams = await listGroups(org);

  return (
    <>
      <PageHeader
        title="Teams"
        description="People and agents of this organization, working together."
      >
        <Button asChild>
          <Link href={`${orgHref(org, "teams")}/${NEW_ITEM}`}>Create team</Link>
        </Button>
      </PageHeader>
      {teams.status === "unreachable" ? (
        <ApiUnreachable subject="this organization's teams" />
      ) : teams.data.length === 0 ? (
        <EmptyState
          icon={UsersRound}
          title="No teams"
          description="This organization has no teams."
        />
      ) : (
        <TeamList org={org} teams={teams.data} />
      )}
    </>
  );
}
