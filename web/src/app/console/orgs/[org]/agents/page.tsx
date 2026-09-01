import type { Metadata } from "next";
import { Bot, ClipboardList } from "lucide-react";

import { Separator } from "@/components/ui/separator";
import { listAgentDefinitions, listAgents, orgNotFound } from "@/features/orgs";

import { ApiUnreachable } from "../../../_components/api-unreachable";
import { EmptyState } from "../../../_components/empty-state";
import { PageHeader } from "../../../_components/page-header";
import { AgentList } from "./_components/agent-list";
import { DefineAgentForm } from "./_components/define-agent-form";
import { DefinitionList } from "./_components/definition-list";

/** Head metadata for the agents route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Agents — garam" };

type Params = { params: Promise<{ org: string }> };

/**
 * The agents the organization holds, the definitions it says its agents
 * should be, and the form that says one more. The two lists are
 * separate because the facts are: an agent is what an operator
 * reported, a definition is what the organization decided, and nothing
 * joins them (ADR-0033).
 *
 * Every role that may read a definition may also write one
 * (`agent:define`), so the form is not gated the way the invite form
 * is; the API refuses a caller that holds neither.
 */
export default async function AgentsPage({ params }: Params) {
  const { org } = await params;
  const [agents, definitions] = await Promise.all([
    listAgents(org).catch(orgNotFound),
    listAgentDefinitions(org).catch(orgNotFound),
  ]);

  return (
    <>
      <PageHeader
        title="Agents"
        description="The agents this organization holds and the definitions it says they should be made from. garam learns an agent exists when an operator reports one."
      />
      {agents.status === "unreachable" ||
      definitions.status === "unreachable" ? (
        // One outage is one fact, so the page reports it once rather
        // than once per list.
        <ApiUnreachable subject="this organization's agents" />
      ) : (
        <>
          {agents.data.length === 0 ? (
            <EmptyState
              icon={Bot}
              title="No agents"
              description="No operator has reported an agent to this organization."
            />
          ) : (
            <AgentList agents={agents.data} />
          )}
          <Separator />
          <section className="flex flex-col gap-3">
            <h2 className="font-heading text-lg font-semibold">Definitions</h2>
            <p className="max-w-2xl text-sm text-muted-foreground">
              What this organization says its agents should be. A definition is
              a record: an operator reads it and builds the agent elsewhere.
            </p>
            {definitions.data.length === 0 ? (
              <EmptyState
                icon={ClipboardList}
                title="No agents defined"
                description="This organization has defined no agents."
              />
            ) : (
              <DefinitionList definitions={definitions.data} />
            )}
          </section>
        </>
      )}
      <Separator />
      <DefineAgentForm org={org} />
    </>
  );
}
