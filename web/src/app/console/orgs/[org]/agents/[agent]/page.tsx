import type { Metadata } from "next";
import Link from "next/link";

import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import {
  getAgentDefinition,
  getOrg,
  lookupAgents,
  lookupOperators,
  slugFromGrn,
} from "@/features/orgs";

import { ApiUnreachable } from "../../../../_components/api-unreachable";
import { ItemCrumb } from "../../../../_components/item-crumb";
import { orgHref } from "../../../../_components/navigation";
import { PageHeader } from "../../../../_components/page-header";
import { DesiredValues, OperatorName } from "../_components/agent-facts";
import { ReportedState } from "../_components/reported-state";
import { RenameAgentForm } from "./_components/rename-agent-form";

/** Head metadata for an agent's route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Agent — garam" };

type Params = { params: Promise<{ org: string; agent: string }> };

/**
 * One defined agent: what the organization asked for and, apart from
 * it, what operators have reported (ADR-0075). The page reads the
 * definition, so an agent only an operator reported has none. The
 * organization is read for itself, so one the caller cannot read
 * answers 404 (ADR-0011); inside it, a refused definition says so.
 */
export default async function AgentPage({ params }: Params) {
  const { org, agent } = await params;
  const [organization, definition, agents, operators] = await Promise.all([
    getOrg(org),
    getAgentDefinition(org, agent),
    lookupAgents(org),
    lookupOperators(org),
  ]);

  if (
    organization.status === "unreachable" ||
    definition.status === "unreachable" ||
    agents.status === "unreachable" ||
    operators.status === "unreachable"
  ) {
    return (
      <>
        <PageHeader
          title="Agent"
          description="An agent of this organization."
        />
        <ApiUnreachable subject="this agent" />
      </>
    );
  }
  if (definition.status === "forbidden") {
    return (
      <>
        <PageHeader
          title="Agent"
          description="An agent of this organization."
        />
        <p role="alert" className="text-sm text-muted-foreground">
          garam refused to show this agent to you.
        </p>
      </>
    );
  }

  const { name, agentGrn, operatorGrn, values, createdAt } = definition.data;
  const reported =
    agents.status === "ok"
      ? agents.data.find((candidate) => candidate.grn === agentGrn)
      : undefined;
  const operatorName =
    operators.status === "ok"
      ? operators.data.find((operator) => operator.grn === operatorGrn)?.name
      : undefined;

  return (
    <>
      <ItemCrumb label={name} />
      <PageHeader title={name} description={`Identifier ${agent}`}>
        <Button asChild variant="outline">
          <Link
            href={`${orgHref(org, "permissions")}/attachments?subject=${encodeURIComponent(`agent:${agent}`)}`}
          >
            Manage policies
          </Link>
        </Button>
      </PageHeader>

      <section className="flex flex-col gap-3">
        <h2 className="font-heading text-lg font-semibold">Reported</h2>
        <ReportedState
          reported={
            agents.status === "forbidden"
              ? "unchecked"
              : reported
                ? "reported"
                : "awaiting"
          }
          agent={reported}
        />
      </section>

      <section className="flex flex-col gap-3">
        <h2 className="font-heading text-lg font-semibold">Requested</h2>
        <dl className="grid max-w-2xl grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
          <dt className="text-muted-foreground">Operator</dt>
          <dd>
            <OperatorName
              org={org}
              identifier={slugFromGrn(operatorGrn)}
              name={operatorName}
            />
          </dd>
          <dt className="text-muted-foreground">Created</dt>
          <dd>
            <time dateTime={createdAt}>{createdAt.slice(0, 10)}</time>
          </dd>
        </dl>
        <h3 className="text-sm font-medium">Sherlock settings</h3>
        <DesiredValues values={values} />
        <p className="max-w-2xl text-sm text-muted-foreground">
          Stored as requested. Whether the operator applied them is not reported
          to garam.
        </p>
      </section>

      <Separator />
      <section className="flex flex-col gap-3">
        <h2 className="font-heading text-lg font-semibold">Settings</h2>
        <RenameAgentForm org={org} agent={agent} name={name} />
      </section>
    </>
  );
}
