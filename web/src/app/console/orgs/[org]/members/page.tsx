import { UsersRound } from "lucide-react";
import type { Metadata } from "next";
import Link from "next/link";

import { Button } from "@/components/ui/button";
import {
  UNNAMED_AGENT,
  directoryAgents,
  getOrg,
  listGroupMembers,
  listPolicies,
  lookupAgentDefinitions,
  lookupAgents,
  lookupGroups,
  lookupOperators,
  lookupOrgMembers,
  slugFromGrn,
} from "@/features/orgs";
import {
  MAX_PERMISSION_CHECKS,
  checkPermissions,
} from "@/lib/permission-check";

import {
  agentHealth,
  holderOf,
  isFailingHealth,
  requestedDot,
} from "../../../_components/agent-status";
import { ApiUnreachable } from "../../../_components/api-unreachable";
import { EmptyState } from "../../../_components/empty-state";
import { InactiveButton } from "../../../_components/inactive-button";
import { orgHref } from "../../../_components/navigation";
import { RefusedRead } from "../../../_components/refused-read";
import { ListPage } from "../../../_components/templates/list-page";
import { InviteDialog } from "./_components/invite-dialog";
import {
  MemberList,
  type Choice,
  type MemberFilters,
  type MemberRow,
} from "./_components/member-list";

/** Head metadata for the members route; Next.js reads it by name. */
export const metadata: Metadata = { title: "People and agents — garam" };

type Params = {
  params: Promise<{ org: string }>;
  searchParams: Promise<{ kind?: string; team?: string; runs?: string }>;
};

const TITLE = "People and agents";
const DESCRIPTION =
  "Everyone who works in this organization. An agent is a member like a person: it sits in teams and holds policies.";

const ROLE_LABEL = { owner: "Owner", admin: "Admin", member: "Member" };

/** The runs-on filter's value for an agent nothing holds. */
const NOWHERE = "nowhere";
/** The runs-on filter's value for an agent a standalone host holds. */
const STANDALONE = "standalone";

/**
 * The organization's people and agents in one list (ADR-0113), told
 * apart by shape. It filters by kind, by team and by what an agent runs
 * on, and acts on several at once: adding them to a team or attaching a
 * policy, offered only for the teams and policies the server does not
 * deny, in one check. A list garam refused is said, never shown as
 * empty. The organization is read for itself, so one the caller cannot
 * read answers 404 (ADR-0011).
 */
export default async function MembersPage({ params, searchParams }: Params) {
  const [{ org }, query] = await Promise.all([params, searchParams]);
  const kind =
    query.kind === "person" || query.kind === "agent" ? query.kind : "all";
  const [
    organization,
    people,
    agents,
    definitions,
    operators,
    groups,
    policies,
    team,
  ] = await Promise.all([
    getOrg(org),
    lookupOrgMembers(org),
    lookupAgents(org),
    lookupAgentDefinitions(org),
    lookupOperators(org),
    lookupGroups(org),
    listPolicies(org),
    query.team ? listGroupMembers(org, query.team) : null,
  ]);

  const teams = groups.status === "ok" ? groups.data : [];
  const policyList = policies.status === "ok" ? policies.data : [];
  // One check for the header's two actions and every team and policy a
  // bulk action could name; past the operation's limit the rest are
  // offered and left to the server, as an unanswered check is.
  const pairs = [
    ["invite", { action: "org:invite" }],
    ["create", { action: "agent:create" }],
    ...teams.map((group) => [
      `team:${slugFromGrn(group.grn)}`,
      {
        action: "group:member-add",
        resource: { type: "group", id: slugFromGrn(group.grn) },
      },
    ]),
    ...policyList.map((policy) => [
      `policy:${slugFromGrn(policy.grn)}`,
      {
        action: "policy:attach",
        resource: { type: "policy", id: slugFromGrn(policy.grn) },
      },
    ]),
  ].slice(0, MAX_PERMISSION_CHECKS) as [
    string,
    Parameters<typeof checkPermissions>[1][string],
  ][];
  const may = await checkPermissions(org, Object.fromEntries(pairs));

  const invite =
    may.get("invite") === "denied" ? (
      <InactiveButton reason="Inviting people needs org:invite, and no policy grants it to you.">
        Invite people
      </InactiveButton>
    ) : (
      <InviteDialog org={org} />
    );
  const newAgent =
    may.get("create") === "denied" ? (
      <InactiveButton reason="Creating an agent needs agent:create, and no policy grants it to you.">
        New agent
      </InactiveButton>
    ) : (
      <Button asChild variant="outline">
        <Link href={orgHref(org, "agents/new")}>New agent</Link>
      </Button>
    );
  const actions = (
    <>
      {newAgent}
      {invite}
    </>
  );

  if (
    organization.status === "unreachable" ||
    people.status === "unreachable" ||
    agents.status === "unreachable" ||
    definitions.status === "unreachable"
  ) {
    return (
      <ListPage title={TITLE} description={DESCRIPTION} primaryAction={actions}>
        <ApiUnreachable subject="this organization's people and agents" />
      </ListPage>
    );
  }

  const operatorNames = new Map(
    (operators.status === "ok" ? operators.data : []).map((operator) => [
      operator.grn,
      operator.name,
    ]),
  );
  const reported = new Map(
    (agents.status === "ok" ? agents.data : []).map((agent) => [
      agent.grn,
      agent,
    ]),
  );
  const definitionsRead = definitions.status === "ok";
  const definitionOf = new Map(
    (definitions.status === "ok" ? definitions.data : []).map((definition) => [
      definition.agentGrn,
      definition,
    ]),
  );

  const personRows: MemberRow[] = (
    people.status === "ok" ? people.data : []
  ).map((person) => {
    const identifier = slugFromGrn(person.userGrn);
    return {
      kind: "person",
      grn: person.userGrn,
      identifier,
      name: person.nickname ?? "No name on record",
      href: orgHref(org, `members/${identifier}`),
      status: { tone: "neutral", label: ROLE_LABEL[person.role] },
      marker: null,
      detail: null,
      runsOn: null,
      lastSeen: null,
    };
  });
  const agentRows: MemberRow[] = directoryAgents({ agents, definitions }).map(
    (entry) => {
      const agent = reported.get(entry.grn);
      const health = agentHealth(entry.reported, agent);
      const holder = holderOf(agent, operatorNames);
      const definition = definitionOf.get(entry.grn);
      return {
        kind: "agent",
        grn: entry.grn,
        identifier: entry.identifier,
        name:
          entry.name ??
          (definitionsRead ? UNNAMED_AGENT : "Name not available"),
        href: orgHref(org, `agents/${entry.identifier}`),
        status: { tone: health.tone, label: health.label },
        // A cutover is marked wherever definitions are listed (ADR-0086).
        marker:
          definition?.cutover != null
            ? requestedDot(definition, definitionsRead)
            : null,
        detail: health.detail,
        runsOn:
          holder === null
            ? { key: NOWHERE, label: "Nowhere yet" }
            : holder.kind === "operator"
              ? {
                  key: slugFromGrn(agent?.assignment?.operatorGrn ?? ""),
                  label: `${holder.name}, operator, epoch ${holder.epoch}`,
                }
              : {
                  key: STANDALONE,
                  label: `${holder.name}, standalone host, epoch ${holder.epoch}`,
                },
        lastSeen: agent?.updatedAt ?? null,
      };
    },
  );

  const inTeam =
    team?.status === "ok"
      ? new Set(team.data.map((member) => member.memberGrn))
      : null;
  // Failing agents lead the agents, each group in the lists' order.
  const all = [
    ...personRows,
    ...agentRows.filter((row) => isFailingHealth(row.status)),
    ...agentRows.filter((row) => !isFailingHealth(row.status)),
  ];
  const rows = all.filter(
    (row) =>
      (kind === "all" || row.kind === kind) &&
      (inTeam === null || inTeam.has(row.grn)) &&
      (!query.runs || row.runsOn?.key === query.runs),
  );

  const filters: MemberFilters = {
    kind,
    team: query.team ?? "",
    runs: query.runs ?? "",
    counts: {
      all: all.length,
      person: personRows.length,
      agent: agentRows.length,
    },
    teams: teams.map((group) => ({
      value: slugFromGrn(group.grn),
      label: group.name,
    })),
    runsOn: [
      ...(operators.status === "ok" ? operators.data : []).map((operator) => ({
        value: slugFromGrn(operator.grn),
        label: `${operator.name} (operator)`,
      })),
      { value: STANDALONE, label: "A standalone host" },
      { value: NOWHERE, label: "Nowhere yet" },
    ],
  };
  const offered = (prefix: string, choices: Choice[]) =>
    choices.filter(
      (choice) => may.get(`${prefix}:${choice.value}`) !== "denied",
    );

  return (
    <ListPage
      title={TITLE}
      description={DESCRIPTION}
      count={
        people.status === "ok" && agents.status === "ok" && definitionsRead
          ? all.length
          : undefined
      }
      primaryAction={actions}
    >
      {people.status === "forbidden" && (
        <RefusedRead subject="this organization's people">
          The list shows agents alone.
        </RefusedRead>
      )}
      {definitions.status === "forbidden" && (
        <RefusedRead subject="this organization's agent definitions">
          The list leaves out agents awaiting an operator.
        </RefusedRead>
      )}
      {agents.status === "forbidden" && (
        <RefusedRead subject="this organization's reported agents">
          The list shows no reported state.
        </RefusedRead>
      )}
      {team !== null && team.status !== "ok" && (
        <RefusedRead subject="this team's members">
          The team filter is not applied.
        </RefusedRead>
      )}
      <MemberList
        org={org}
        rows={rows}
        filters={filters}
        teamChoices={offered("team", filters.teams)}
        policyChoices={offered(
          "policy",
          policyList.map((policy) => ({
            value: slugFromGrn(policy.grn),
            label: policy.name,
          })),
        )}
        empty={
          <EmptyState
            icon={UsersRound}
            title="No one here"
            description={
              all.length === 0
                ? "This organization has no people or agents you can list."
                : "No one matches these filters."
            }
          />
        }
      />
    </ListPage>
  );
}
