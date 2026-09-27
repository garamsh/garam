import type { Metadata } from "next";
import { UsersRound } from "lucide-react";

import { Separator } from "@/components/ui/separator";
import {
  getGroup,
  listAgents,
  listGroupMembers,
  listOrgMembers,
} from "@/features/orgs";

import { ApiUnreachable } from "../../../../_components/api-unreachable";
import { EmptyState } from "../../../../_components/empty-state";
import { ItemCrumb } from "../../../../_components/item-crumb";
import { PageHeader } from "../../../../_components/page-header";
import { AddTeamMemberForm } from "./_components/add-team-member-form";
import { DeleteTeamForm } from "./_components/delete-team-form";
import { RenameTeamForm } from "./_components/rename-team-form";
import { TeamMemberTable } from "./_components/team-member-table";
import { resolveTeamMembers, teamCandidates } from "./_components/team-member";

/** Head metadata for a team's route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Team — garam" };

type Params = { params: Promise<{ org: string; team: string }> };

/**
 * One team: who is in it, who can be added, and its settings.
 *
 * The organization's people and agents are read beside the team so
 * each membership, which is a bare GRN, can be shown by name and type.
 * An organization the caller cannot read answers 404 through those
 * reads (ADR-0011); a team the caller is refused inside one it can read
 * says so instead (ADR-0072).
 *
 * The write controls are rendered for every reader and the API refuses
 * a caller that holds only the read, as in the other sections.
 */
export default async function TeamPage({ params }: Params) {
  const { org, team } = await params;
  const [group, members, people, agents] = await Promise.all([
    getGroup(org, team),
    listGroupMembers(org, team),
    listOrgMembers(org),
    listAgents(org),
  ]);

  if (
    group.status === "unreachable" ||
    members.status === "unreachable" ||
    people.status === "unreachable" ||
    agents.status === "unreachable"
  ) {
    return (
      <>
        <PageHeader title="Team" description="A team of this organization." />
        <ApiUnreachable subject="this team" />
      </>
    );
  }
  if (group.status === "forbidden" || members.status === "forbidden") {
    return (
      <>
        <PageHeader title="Team" description="A team of this organization." />
        <section role="alert" className="flex flex-col gap-1">
          <h2 className="font-heading text-lg font-semibold">
            You cannot view this team
          </h2>
          <p className="text-sm text-muted-foreground">
            garam refused to show this team to you. Access to it is granted by
            this organization&apos;s roles and policies.
          </p>
        </section>
      </>
    );
  }

  const name = group.data.name;
  const resolved = resolveTeamMembers(members.data, people.data, agents.data);

  return (
    <>
      <ItemCrumb label={name} />
      <PageHeader
        title={name}
        description="People and agents of this organization in one team."
      />
      <section className="flex flex-col gap-3">
        <h2 className="font-heading text-lg font-semibold">Members</h2>
        {resolved.length === 0 ? (
          <EmptyState
            icon={UsersRound}
            title="No members"
            description="This team has no members."
          />
        ) : (
          <TeamMemberTable
            org={org}
            team={team}
            teamName={name}
            members={resolved}
          />
        )}
      </section>
      <AddTeamMemberForm
        org={org}
        team={team}
        candidates={teamCandidates(members.data, people.data, agents.data)}
      />
      <Separator />
      <section className="flex flex-col gap-4">
        <h2 className="font-heading text-lg font-semibold">Settings</h2>
        <RenameTeamForm org={org} team={team} name={name} />
        <DeleteTeamForm
          org={org}
          team={team}
          teamName={name}
          memberCount={resolved.length}
        />
      </section>
    </>
  );
}
