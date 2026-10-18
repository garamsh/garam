import type { Metadata } from "next";
import Link from "next/link";
import { Bot, Plus } from "lucide-react";

import { Banner } from "@/components/banner";
import { Button } from "@/components/ui/button";
import {
  UNNAMED_AGENT,
  directoryAgents,
  getOrg,
  isFailing,
  lookupAgentDefinitions,
  lookupAgents,
  lookupOperators,
  slugFromGrn,
} from "@/features/orgs";
import { checkPermissions } from "@/lib/permission-check";

import { ApiUnreachable } from "../../../_components/api-unreachable";
import { EmptyState } from "../../../_components/empty-state";
import { RefusedRead } from "../../../_components/refused-read";
import { ListPage } from "../../../_components/templates/list-page";
import { reportedDots, requestedDot } from "./_components/agent-status";
import { AgentTable, type AgentRow } from "./_components/agent-table";

/** Head metadata for the agents route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Agents — garam" };

type Params = { params: Promise<{ org: string }> };

const TITLE = "Agents";
const DESCRIPTION =
  "Agents this organization defined, and what operators report about them.";

/**
 * The organization's agents: each one it defined, whether or not an
 * operator has claimed it yet, beside what operators have reported
 * (ADR-0075). What the organization asked for and what was reported
 * are read from different lists and shown apart (ADR-0033); a defined
 * agent no operator has reported is said to be awaiting one rather
 * than drawn with a state nobody reported. Failing agents lead.
 *
 * The organization is read for itself, so one the caller cannot read
 * answers 404 (ADR-0011); a refused list inside it is said as such.
 * "New agent" is asked of the server and left out only where it says
 * no (ADR-0103).
 */
export default async function AgentsPage({ params }: Params) {
  const { org } = await params;
  const [organization, agents, definitions, operators, permissions] =
    await Promise.all([
      getOrg(org),
      lookupAgents(org),
      lookupAgentDefinitions(org),
      lookupOperators(org),
      checkPermissions(org, { create: { action: "agent:create" } }),
    ]);

  const newAgent =
    permissions.get("create") === "denied" ? undefined : (
      <Button asChild>
        <Link href={`/console/orgs/${org}/agents/new`}>
          <Plus aria-hidden />
          New agent
        </Link>
      </Button>
    );
  if (
    organization.status === "unreachable" ||
    agents.status === "unreachable" ||
    definitions.status === "unreachable" ||
    operators.status === "unreachable"
  ) {
    return (
      <ListPage
        title={TITLE}
        description={DESCRIPTION}
        primaryAction={newAgent}
      >
        <ApiUnreachable subject="this organization's agents" />
      </ListPage>
    );
  }

  const definitionsRead = definitions.status === "ok";
  const reported = new Map(
    (agents.status === "ok" ? agents.data : []).map((agent) => [
      agent.grn,
      agent,
    ]),
  );
  const definitionOf = new Map(
    (definitions.status === "ok" ? definitions.data : []).map((d) => [
      d.agentGrn,
      d,
    ]),
  );
  const operatorNames = new Map(
    (operators.status === "ok" ? operators.data : []).map((operator) => [
      operator.grn,
      operator.name,
    ]),
  );
  const rows = directoryAgents({ agents, definitions }).map((entry) => {
    const definition = definitionOf.get(entry.grn);
    const agent = reported.get(entry.grn);
    const operatorGrn = definition?.operatorGrn;
    const row: AgentRow = {
      identifier: entry.identifier,
      // A reported agent's name is its definition's, so where the
      // definitions were not read its name is unknown, not absent.
      name:
        entry.name ?? (definitionsRead ? UNNAMED_AGENT : "Name not available"),
      nameNote:
        entry.name !== null
          ? null
          : definitionsRead
            ? "Reported by an operator without a definition here."
            : "Reported by an operator. Its definition is not checked.",
      operator:
        operatorGrn === undefined
          ? null
          : {
              identifier: slugFromGrn(operatorGrn),
              name: operatorNames.get(operatorGrn),
            },
      requested: requestedDot(definition, definitionsRead),
      ...reportedDots(entry.reported, agent),
      updatedAt: agent?.updatedAt ?? null,
    };
    return { row, failing: isFailing(agent) };
  });
  // Failing first, each group keeping the order the lists gave it.
  const failing = rows.filter((entry) => entry.failing).map((e) => e.row);
  const ordered = [
    ...failing,
    ...rows.filter((entry) => !entry.failing).map((e) => e.row),
  ];
  const partial =
    definitions.status === "forbidden" || agents.status === "forbidden";

  return (
    <ListPage
      title={TITLE}
      description={DESCRIPTION}
      count={partial ? undefined : ordered.length}
      primaryAction={newAgent}
    >
      {failing.length > 0 && (
        <Banner
          tone="danger"
          title={
            failing.length === 1
              ? "1 agent is failing"
              : `${failing.length} agents are failing`
          }
        >
          {failing.map((row) => row.name).join(", ")}: provisioning failed or
          the agent disconnected. Failing agents are listed first.
        </Banner>
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
      <AgentTable
        org={org}
        rows={ordered}
        empty={
          <EmptyState
            icon={Bot}
            title="No agents"
            description={
              partial
                ? "None you can list."
                : "This organization has no agents."
            }
          />
        }
      />
    </ListPage>
  );
}
