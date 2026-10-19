import { Bot } from "lucide-react";
import type { Metadata } from "next";
import Link from "next/link";

import { Button } from "@/components/ui/button";
import {
  UNNAMED_AGENT,
  directoryAgents,
  getOrg,
  lookupAgents,
  lookupOperators,
  slugFromGrn,
} from "@/features/orgs";

import {
  agentHealth,
  holderOf,
  isFailingHealth,
} from "../../../_components/agent-status";
import { ApiUnreachable } from "../../../_components/api-unreachable";
import { EmptyState } from "../../../_components/empty-state";
import { InactiveButton } from "../../../_components/inactive-button";
import { readMemberChoices } from "../../../_components/member-choices";
import { MemberList, type MemberRow } from "../../../_components/member-list";
import { orgHref } from "../../../_components/navigation";
import { RefusedRead } from "../../../_components/refused-read";
import { ListPage } from "../../../_components/templates/list-page";

/** Head metadata for the agents route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Agents — garam" };

type Params = {
  params: Promise<{ org: string }>;
  searchParams: Promise<{ team?: string; runs?: string }>;
};

const TITLE = "Agents";

/** The runs-on filter's value for an agent nothing holds. */
const NOWHERE = "nowhere";
/** The runs-on filter's value for an agent a standalone host holds. */
const STANDALONE = "standalone";

/**
 * The organization's agents (ADR-0116), failing ones first, filtered
 * by team and by what runs them, and added to a team or given a policy
 * several at once. Creating one is the page's primary action. A list
 * garam refused is said, never shown as empty. The organization is read
 * for itself, so one the caller cannot read answers 404 (ADR-0011).
 */
export default async function AgentsPage({ params, searchParams }: Params) {
  const [{ org }, query] = await Promise.all([params, searchParams]);
  const [organization, agents, operators, choices] = await Promise.all([
    getOrg(org),
    lookupAgents(org),
    lookupOperators(org),
    readMemberChoices(org, query.team, {
      create: { action: "agent:create" },
    }),
  ]);

  const create =
    choices.may.get("create") === "denied" ? (
      <InactiveButton
        variant="default"
        reason="No policy lets you create agents."
      >
        New agent
      </InactiveButton>
    ) : (
      <Button asChild>
        <Link href={orgHref(org, "agents/new")}>New agent</Link>
      </Button>
    );

  if (
    organization.status === "unreachable" ||
    agents.status === "unreachable"
  ) {
    return (
      <ListPage title={TITLE} primaryAction={create}>
        <ApiUnreachable subject="this organization's agents" />
      </ListPage>
    );
  }
  if (agents.status === "forbidden") {
    return (
      <ListPage title={TITLE} primaryAction={create}>
        <RefusedRead subject="this organization's agents" />
      </ListPage>
    );
  }

  const operatorList = operators.status === "ok" ? operators.data : [];
  const operatorNames = new Map(
    operatorList.map((operator) => [operator.grn, operator.name]),
  );
  const reported = new Map(agents.data.map((agent) => [agent.grn, agent]));
  const all: MemberRow[] = directoryAgents({ agents }).map((entry) => {
    const agent = reported.get(entry.grn);
    const health = agentHealth(entry.reported, agent);
    const holder = holderOf(agent, operatorNames);
    return {
      kind: "agent",
      grn: entry.grn,
      identifier: entry.identifier,
      name: entry.name ?? UNNAMED_AGENT,
      href: orgHref(org, `agents/${entry.identifier}`),
      status: { tone: health.tone, label: health.label },
      detail: health.detail,
      runsOn:
        holder === null
          ? { key: NOWHERE, label: "Nowhere yet" }
          : holder.kind === "operator"
            ? {
                key: slugFromGrn(agent?.assignment?.assigneeGrn ?? ""),
                label: holder.name,
              }
            : { key: STANDALONE, label: `${holder.name} (standalone)` },
      lastSeen: agent?.updatedAt ?? null,
    };
  });
  // Failing agents lead, each group in the lists' order.
  const ordered = [
    ...all.filter((row) => isFailingHealth(row.status)),
    ...all.filter((row) => !isFailingHealth(row.status)),
  ];
  const rows = ordered.filter(
    (row) =>
      (choices.inTeam === null || choices.inTeam.has(row.grn)) &&
      (!query.runs || row.runsOn?.key === query.runs),
  );

  return (
    <ListPage title={TITLE} count={all.length} primaryAction={create}>
      {choices.teamUnread && (
        <RefusedRead subject="this team's members">
          The team filter is not applied.
        </RefusedRead>
      )}
      <MemberList
        org={org}
        kind="agent"
        rows={rows}
        filters={{
          team: query.team ?? "",
          runs: query.runs ?? "",
          teams: choices.teams,
          runsOn: [
            ...operatorList.map((operator) => ({
              value: slugFromGrn(operator.grn),
              label: operator.name,
            })),
            { value: STANDALONE, label: "A standalone host" },
            { value: NOWHERE, label: "Nowhere yet" },
          ],
        }}
        teamChoices={choices.teamChoices}
        policyChoices={choices.policyChoices}
        empty={
          <EmptyState
            icon={Bot}
            title={all.length === 0 ? "No agents yet." : "No agent matches."}
            action={all.length === 0 ? create : undefined}
          />
        }
      />
    </ListPage>
  );
}
