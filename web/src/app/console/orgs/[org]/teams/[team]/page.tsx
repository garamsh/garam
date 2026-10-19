import type { Metadata } from "next";
import Link from "next/link";

import { Button } from "@/components/ui/button";
import {
  KIND_LABEL,
  getGroup,
  getOrg,
  listGroupMembers,
  lookupAgentDefinitions,
  lookupAgents,
  lookupOrgMembers,
  memberLabel,
  memberName,
  refusedAgentLists,
  resolveTeamMembers,
  teamCandidates,
} from "@/features/orgs";

import { ApiUnreachable } from "../../../../_components/api-unreachable";
import { ItemCrumb } from "../../../../_components/item-crumb";
import { orgHref } from "../../../../_components/navigation";
import { PageHeader } from "../../../../_components/page-header";
import { RefusedRead } from "../../../../_components/refused-read";
import { DetailPage } from "../../../../_components/templates/detail-page";
import {
  YourAccess,
  accessDecisions,
  readAccess,
} from "../../../../_components/your-access";
import { AddMemberForm } from "../../../../_components/membership/add-member-form";
import { DeleteTeamDialog } from "./_components/delete-team-dialog";
import { RenameTeamForm } from "./_components/rename-team-form";
import { MemberTable } from "../../../../_components/membership/member-table";

/** Head metadata for a team's route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Team — garam" };

type Params = { params: Promise<{ org: string; team: string }> };

const FALLBACK_TITLE = "Team";
const FALLBACK_DESCRIPTION = "A team of this organization.";

/**
 * One team: who is in it and who can be added, then its settings, with
 * deleting it in the danger zone.
 *
 * The organization's people and agents are read beside the team so
 * each membership, which is a bare GRN, can be shown by name and type.
 * The organization is read for itself, so one the caller cannot read
 * answers 404 (ADR-0011). Inside one it can read, a refused team says
 * so, and a refused list of people or agents leaves those members
 * shown by identifier and marked unchecked (ADR-0072).
 *
 * The page asks the server once which of its writes the caller may
 * take, and leaves out only those it answers are denied.
 */
export default async function TeamPage({ params }: Params) {
  const { org, team } = await params;
  const group = { type: "group", id: team } as const;
  // Started before the page's own reads, so it runs beside them.
  const access = readAccess(org, [
    {
      key: "rename",
      label: "Rename it",
      check: { action: "group:update", resource: group },
    },
    {
      key: "add",
      label: "Add members",
      check: { action: "group:member-add", resource: group },
    },
    {
      key: "remove",
      label: "Remove members",
      check: { action: "group:member-remove", resource: group },
    },
    {
      key: "delete",
      label: "Delete it",
      check: { action: "group:delete", resource: group },
    },
  ]);
  const [organization, read, members, people, agents, definitions, readings] =
    await Promise.all([
      getOrg(org),
      getGroup(org, team),
      listGroupMembers(org, team),
      lookupOrgMembers(org),
      lookupAgents(org),
      lookupAgentDefinitions(org),
      access,
    ]);
  const may = accessDecisions(readings);

  if (
    organization.status === "unreachable" ||
    read.status === "unreachable" ||
    members.status === "unreachable" ||
    people.status === "unreachable" ||
    agents.status === "unreachable" ||
    definitions.status === "unreachable"
  ) {
    return (
      <div className="flex flex-col gap-6">
        <PageHeader title={FALLBACK_TITLE} description={FALLBACK_DESCRIPTION} />
        <ApiUnreachable subject="this team" />
      </div>
    );
  }
  if (read.status === "forbidden" || members.status === "forbidden") {
    return (
      <div className="flex flex-col gap-6">
        <PageHeader title={FALLBACK_TITLE} description={FALLBACK_DESCRIPTION} />
        <RefusedRead subject="this team" />
      </div>
    );
  }

  const name = read.data.name;
  const directory = { agents, definitions };
  const resolved = resolveTeamMembers(members.data, people, directory);
  // Agents are known by two lists. Both refused, no agent can be
  // chosen; one refused, the other still offers some, and the page says
  // some may be missing rather than none can be chosen (ADR-0075).
  const refusedAgents = refusedAgentLists(directory);
  const agentsUnlisted = refusedAgents.length === 2;
  const unlisted = [
    ...(people.status === "forbidden" ? ["people"] : []),
    ...(agentsUnlisted ? ["agents"] : []),
  ];
  const memberships =
    resolved.length === 1 ? "1 membership" : `${resolved.length} memberships`;

  const overview = (
    <>
      <section aria-labelledby="members-title" className="flex flex-col gap-3">
        <h2 id="members-title" className="text-base">
          Members
        </h2>
        <MemberTable
          org={org}
          scope={{ kind: "team", id: team, name }}
          removable={may.get("remove") !== "denied"}
          members={resolved.map((member) => ({
            grn: member.grn,
            identifier: member.identifier,
            type: KIND_LABEL[member.kind],
            name: memberName(member),
            named: member.label !== null,
            sentence: memberLabel(member),
            lookup: member.lookup,
            listed: member.kind === "agent" ? "agents" : "people",
          }))}
        />
      </section>
      {may.get("add") !== "denied" && (
        <AddMemberForm
          org={org}
          scope={{ kind: "team", id: team, name }}
          candidates={teamCandidates(members.data, people, directory)}
          unlisted={unlisted}
          partlyListed={agentsUnlisted ? [] : refusedAgents}
        />
      )}
    </>
  );

  const settings = may.get("rename") !== "denied" && (
    <section
      aria-labelledby="rename-title"
      className="rounded-xl border bg-card"
    >
      <div className="flex flex-col gap-1 border-b px-4 py-3">
        <h2 id="rename-title" className="text-base">
          Name
        </h2>
        <p className="text-muted-foreground">
          The team&apos;s identifier and members stay as they are.
        </p>
      </div>
      <div className="p-4">
        <RenameTeamForm org={org} team={team} name={name} />
      </div>
    </section>
  );
  const deletable = may.get("delete") !== "denied";

  return (
    <>
      <ItemCrumb label={name} />
      <DetailPage
        name={name}
        idLabel="Team identifier"
        id={team}
        actions={
          <Button asChild variant="outline">
            <Link
              href={`${orgHref(org, "permissions")}/attachments?subject=${encodeURIComponent(`team:${team}`)}`}
            >
              Manage policies
            </Link>
          </Button>
        }
        tabs={[
          { value: "overview", label: "Overview", content: overview },
          ...(settings || deletable
            ? [{ value: "settings", label: "Settings", content: settings }]
            : []),
        ]}
        dangerZone={
          deletable
            ? [
                {
                  title: "Delete team",
                  control: (
                    <DeleteTeamDialog
                      org={org}
                      team={team}
                      teamName={name}
                      memberships={memberships}
                    />
                  ),
                },
              ]
            : undefined
        }
      />
      <YourAccess readings={readings} />
    </>
  );
}
