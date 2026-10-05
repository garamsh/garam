import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import {
  getAgentDefinition,
  getOrg,
  hostedFor,
  listHostedOperators,
  lookupAgents,
  lookupOperators,
  slugFromGrn,
  type AgentDefinition,
} from "@/features/orgs";

import { ApiUnreachable } from "../../../../_components/api-unreachable";
import { ItemCrumb } from "../../../../_components/item-crumb";
import { orgHref } from "../../../../_components/navigation";
import { PageHeader } from "../../../../_components/page-header";
import { DesiredValues, OperatorName } from "../_components/agent-facts";
import { ExecutionPanel } from "../_components/execution-panel";
import { ReportedState } from "../_components/reported-state";
import { RenameAgentForm } from "./_components/rename-agent-form";

/** Head metadata for an agent's route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Agent — garam" };

type Params = { params: Promise<{ org: string; agent: string }> };

/**
 * One agent: what the organization asked for where garam holds a
 * definition, what operators have reported (ADR-0075), and the
 * execution its hosted operator's control service holds (#1170). An
 * agent a control service created has no definition in garam, so its
 * page carries the report and the execution alone; an identifier
 * neither names answers 404. The organization is read for itself, so
 * one the caller cannot read answers 404 (ADR-0011); inside it, a
 * refused definition says so. A control service that is missing or
 * unavailable disables only the execution section.
 */
export default async function AgentPage({ params }: Params) {
  const { org, agent } = await params;
  const [organization, definition, agents, operators, hosted] =
    await Promise.all([
      getOrg(org),
      getAgentDefinition(org, agent),
      lookupAgents(org),
      lookupOperators(org),
      listHostedOperators(org),
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

  const reported =
    agents.status === "ok"
      ? agents.data.find((candidate) =>
          definition.data === null
            ? slugFromGrn(candidate.grn) === agent
            : candidate.grn === definition.data.agentGrn,
        )
      : undefined;
  if (definition.data === null && reported === undefined) notFound();
  const agentGrn = definition.data?.agentGrn ?? reported?.grn ?? "";
  const name = definition.data?.name ?? agent;
  const assignee = reported?.assignment?.assigneeGrn;

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
        <h2 className="font-heading text-lg font-semibold">Execution</h2>
        {hosted.status === "ok" ? (
          <ExecutionPanel
            org={org}
            agentGrn={agentGrn}
            hosted={hostedFor(hosted.data, "agent:execution-read", assignee)}
          />
        ) : (
          <p role="status" className="text-sm text-muted-foreground">
            garam did not answer for this organization&apos;s control services.
          </p>
        )}
      </section>

      {definition.data !== null && (
        <Requested
          org={org}
          agent={agent}
          definition={definition.data}
          operatorName={
            operators.status === "ok"
              ? operators.data.find(
                  (operator) => operator.grn === definition.data?.operatorGrn,
                )?.name
              : undefined
          }
        />
      )}
    </>
  );
}

/** What the organization asked for, and renaming it. */
function Requested({
  org,
  agent,
  definition,
  operatorName,
}: {
  org: string;
  agent: string;
  definition: AgentDefinition;
  operatorName: string | undefined;
}) {
  const { name, operatorGrn, values, createdAt } = definition;
  return (
    <>
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
        <h3 className="text-sm font-medium">Settings</h3>
        <DesiredValues values={values} />
        <p className="max-w-2xl text-sm text-muted-foreground">
          Saved values; application is not reported.
        </p>
      </section>

      <Separator />
      <section className="flex flex-col gap-3">
        <h2 className="font-heading text-lg font-semibold">Rename</h2>
        <RenameAgentForm org={org} agent={agent} name={name} />
      </section>
    </>
  );
}
