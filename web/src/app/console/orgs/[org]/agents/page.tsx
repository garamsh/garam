import type { Metadata } from "next";
import Link from "next/link";
import { Bot } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  directoryAgents,
  getOrg,
  lookupAgentDefinitions,
  lookupAgents,
  lookupOperators,
  slugFromGrn,
} from "@/features/orgs";

import { ApiUnreachable } from "../../../_components/api-unreachable";
import { EmptyState } from "../../../_components/empty-state";
import { PageHeader } from "../../../_components/page-header";
import { AgentRoster, type RosterEntry } from "./_components/agent-roster";
import { AgentsTabs } from "./_components/agents-tabs";

/** Head metadata for the agents route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Agents — garam" };

type Params = { params: Promise<{ org: string }> };

/**
 * The organization's agents: each one it defined, whether or not an
 * operator has claimed it yet, beside what operators have reported
 * (ADR-0075). What the organization asked for and what was reported
 * are read from different lists and shown apart (ADR-0033); a defined
 * agent no operator has reported is said to be awaiting one rather
 * than drawn with a state nobody reported.
 *
 * The organization is read for itself, so one the caller cannot read
 * answers 404 (ADR-0011); a refused list inside it is said as such.
 */
export default async function AgentsPage({ params }: Params) {
  const { org } = await params;
  const [organization, agents, definitions, operators] = await Promise.all([
    getOrg(org),
    lookupAgents(org),
    lookupAgentDefinitions(org),
    lookupOperators(org),
  ]);

  const header = (
    <>
      <PageHeader
        title="Agents"
        description="Agents this organization defined, and what operators report about them."
      >
        <Button asChild>
          <Link href={`/console/orgs/${org}/agents/new`}>New agent</Link>
        </Button>
      </PageHeader>
      <AgentsTabs org={org} current="agents" />
    </>
  );
  if (
    organization.status === "unreachable" ||
    agents.status === "unreachable" ||
    definitions.status === "unreachable" ||
    operators.status === "unreachable"
  ) {
    return (
      <>
        {header}
        <ApiUnreachable subject="this organization's agents" />
      </>
    );
  }

  const directory = { agents, definitions };
  const reported = new Map(
    (agents.status === "ok" ? agents.data : []).map((agent) => [
      agent.grn,
      agent,
    ]),
  );
  const operatorOf = new Map(
    (definitions.status === "ok" ? definitions.data : []).map((d) => [
      d.agentGrn,
      d.operatorGrn,
    ]),
  );
  const operatorNames = new Map(
    (operators.status === "ok" ? operators.data : []).map((operator) => [
      operator.grn,
      operator.name,
    ]),
  );
  const entries: RosterEntry[] = directoryAgents(directory).map((entry) => {
    const operatorGrn = operatorOf.get(entry.grn);
    return {
      directory: entry,
      agent: reported.get(entry.grn),
      operator:
        operatorGrn === undefined
          ? undefined
          : {
              identifier: slugFromGrn(operatorGrn),
              name: operatorNames.get(operatorGrn),
            },
    };
  });

  return (
    <>
      {header}
      {definitions.status === "forbidden" && (
        <p role="status" className="text-sm text-muted-foreground">
          Agent definitions are not listed to you, so agents awaiting an
          operator are not shown.
        </p>
      )}
      {agents.status === "forbidden" && (
        <p role="status" className="text-sm text-muted-foreground">
          Reported agents are not listed to you, so no reported state is shown.
        </p>
      )}
      <section id="definitions" className="scroll-mt-4">
        {entries.length === 0 ? (
          <EmptyState
            icon={Bot}
            title="No agents"
            description={
              definitions.status === "forbidden" ||
              agents.status === "forbidden"
                ? "None you can list."
                : "Create one to ask an operator to run it."
            }
          />
        ) : (
          <AgentRoster org={org} entries={entries} />
        )}
      </section>
    </>
  );
}
