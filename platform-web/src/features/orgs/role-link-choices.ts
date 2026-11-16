import { agentChoiceLabel, directoryAgents } from "./agent-directory";
import type { Agent, Group, Member, Project } from "./api/orgs";
import { slugFromGrn } from "@/lib/grn";

/** One thing the role form offers, under the heading of its kind. */
export type RoleLinkChoice = {
  value: string;
  label: string;
  group: "Teams" | "Agents" | "People" | "Projects";
};

/**
 * Who and what the role form offers, which is what the relay accepts
 * (ADR-0173): at organization scope an agent or a team as principal,
 * never a user, whose organization role is its membership's; at
 * team-or-project scope a user as principal and a team or a project as
 * resource. A list that was not read offers nothing.
 */
export type RoleLinkChoices = {
  organizationPrincipals: RoleLinkChoice[];
  scopedPrincipals: RoleLinkChoice[];
  scopedResources: RoleLinkChoice[];
};

/** What each list the form draws on holds, or null where it was refused. */
export type RoleLinkSources = {
  teams: readonly Group[] | null;
  agents: readonly Agent[] | null;
  members: readonly Member[] | null;
  projects: readonly Project[] | null;
};

/** The choices the role form offers from the lists it was given. */
export function roleLinkChoices({
  teams,
  agents,
  members,
  projects,
}: RoleLinkSources): RoleLinkChoices {
  const teamChoices = (teams ?? []).map((team): RoleLinkChoice => ({
    value: team.grn,
    label: `${team.name} (${slugFromGrn(team.grn)})`,
    group: "Teams",
  }));
  const agentChoices = directoryAgents({
    agents:
      agents === null
        ? { status: "forbidden" }
        : { status: "ok", data: agents },
  }).map((agent): RoleLinkChoice => ({
    value: agent.grn,
    label: agentChoiceLabel(agent),
    group: "Agents",
  }));
  return {
    organizationPrincipals: [...teamChoices, ...agentChoices],
    scopedPrincipals: (members ?? []).map((member): RoleLinkChoice => ({
      value: member.userGrn,
      label: `${member.nickname ?? "No name on record"} (${slugFromGrn(member.userGrn)})`,
      group: "People",
    })),
    scopedResources: [
      ...teamChoices,
      ...(projects ?? []).map((project): RoleLinkChoice => ({
        value: project.grn,
        label: `${project.name} (${slugFromGrn(project.grn)})`,
        group: "Projects",
      })),
    ],
  };
}
