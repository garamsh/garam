import type { Metadata } from "next";
import { Bot } from "lucide-react";

import { Separator } from "@/components/ui/separator";
import { listAgentDefinitions, orgNotFound } from "@/features/orgs";

import { ApiUnreachable } from "../../../_components/api-unreachable";
import { EmptyState } from "../../../_components/empty-state";
import { PageHeader } from "../../../_components/page-header";
import { DefineAgentForm } from "./_components/define-agent-form";
import { DefinitionList } from "./_components/definition-list";

/** Head metadata for the agents route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Agents — garam" };

type Params = { params: Promise<{ org: string }> };

/**
 * The agents the organization says it should have, and the form that
 * says one more. Every role that may read a definition may also write
 * one (`agent:define`), so the form is not gated the way the invite
 * form is; the API refuses a caller that holds neither.
 */
export default async function AgentsPage({ params }: Params) {
  const { org } = await params;
  const definitions = await listAgentDefinitions(org).catch(orgNotFound);

  return (
    <>
      <PageHeader
        title="Agents"
        description="What this organization says its agents should be. A definition is a record: an operator reads it and builds the agent elsewhere."
      />
      {definitions.status === "unreachable" ? (
        <ApiUnreachable subject="this organization's agent definitions" />
      ) : definitions.data.length === 0 ? (
        <EmptyState
          icon={Bot}
          title="No agents defined"
          description="This organization has defined no agents."
        />
      ) : (
        <DefinitionList definitions={definitions.data} />
      )}
      <Separator />
      <DefineAgentForm org={org} />
    </>
  );
}
