import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { ControlNotice, NO_FIX } from "@/components/control-notice";
import { DescriptionList } from "@/components/description-list";
import { MemberMark } from "@/components/member-mark";
import { StatusDot } from "@/components/status-dot";
import { Time } from "@/components/time";
import { Button } from "@/components/ui/button";
import {
  UNNAMED_AGENT,
  getAgentDefinition,
  getOrg,
  hostedFor,
  listHostedOperators,
  lookupAgents,
  lookupOperators,
  lookupOrgMembers,
  slugFromGrn,
  type AgentDefinition,
  type Reported as ReportedState,
} from "@/features/orgs";

import { ApiUnreachable } from "../../../../_components/api-unreachable";
import { ApproveHostDialog } from "../../../../_components/approve-host-dialog";
import { InactiveButton } from "../../../../_components/inactive-button";
import { ItemCrumb } from "../../../../_components/item-crumb";
import { orgHref } from "../../../../_components/navigation";
import { PageHeader } from "../../../../_components/page-header";
import { RefusedRead } from "../../../../_components/refused-read";
import { Section } from "../../../../_components/section";
import {
  DetailPage,
  type DetailTab,
} from "../../../../_components/templates/detail-page";
import { DesiredValues, OperatorName } from "../_components/agent-facts";
import {
  holderOf,
  reportedDots,
  type Holder,
} from "../../../../_components/agent-status";
import {
  YourAccess,
  accessDecisions,
  readAccess,
} from "../../../../_components/your-access";
import { ExecutionPanel } from "../_components/execution-panel";
import { RenameAgentForm } from "./_components/rename-agent-form";

/** Head metadata for an agent's route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Agent — garam" };

type Params = { params: Promise<{ org: string; agent: string }> };

/**
 * One agent: its two reported states and what holds it in the header,
 * with the generation garam last activated where the read answers one
 * (#1576), what the organization defined where garam holds a
 * definition (ADR-0075), and the execution its hosted operator's
 * control service holds (#1170), with the rename under Settings. An agent a
 * control service created has no definition in garam, so its page
 * carries the report and the execution alone; an identifier neither
 * names answers 404. The organization is read for itself, so one the
 * caller cannot read answers 404 (ADR-0011); inside it, a refused
 * definition says so. A control service that is missing or unavailable
 * disables only the Execution tab. The rename is asked of the server
 * and left out only where it says no (ADR-0103).
 */
export default async function AgentPage({ params }: Params) {
  const { org, agent } = await params;
  const self = { type: "agent", id: agent } as const;
  // Started before the page's own reads, so it runs beside them.
  const access = readAccess(org, [
    {
      key: "rename",
      label: "Rename it",
      check: { action: "agent:rename", resource: self },
    },
    {
      key: "enroll",
      label: "Replace its standalone host",
      check: { action: "agent:enroll", resource: self },
    },
  ]);
  const [
    organization,
    definition,
    agents,
    operators,
    members,
    hosted,
    readings,
  ] = await Promise.all([
    getOrg(org),
    getAgentDefinition(org, agent),
    lookupAgents(org),
    lookupOperators(org),
    lookupOrgMembers(org),
    listHostedOperators(org),
    access,
  ]);

  const fallback = <PageHeader title="Agent" />;
  if (
    organization.status === "unreachable" ||
    definition.status === "unreachable" ||
    agents.status === "unreachable" ||
    operators.status === "unreachable"
  ) {
    return (
      <>
        {fallback}
        <ApiUnreachable subject="this agent" />
      </>
    );
  }
  if (definition.status === "forbidden") {
    return (
      <>
        {fallback}
        <RefusedRead subject="this agent" />
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
  // An agent a control service created carries no name in garam, and
  // its identifier is never put in a name's place: the header offers
  // it to copy beside the name instead.
  const name = definition.data?.name || UNNAMED_AGENT;
  const state: ReportedState =
    agents.status === "forbidden"
      ? "unchecked"
      : reported
        ? "reported"
        : "awaiting";
  const dots = reportedDots(state, reported);
  const holder = holderOf(
    reported,
    new Map(
      (operators.status === "ok" ? operators.data : []).map((operator) => [
        operator.grn,
        operator.name,
      ]),
    ),
  );
  const definedBy =
    definition.data === null
      ? null
      : members.status === "ok"
        ? (members.data.find(
            (member) => member.userGrn === definition.data?.definedBy,
          )?.nickname ?? "A person no longer in this organization")
        : "A person whose name is not shown to you";

  const tabs: DetailTab[] = [
    {
      value: "overview",
      label: "Overview",
      content: (
        <>
          {definition.data === null ? (
            <p className="text-muted-foreground">{noDefinitionCause(holder)}</p>
          ) : (
            <Requested
              org={org}
              definition={definition.data}
              operatorName={
                operators.status === "ok"
                  ? operators.data.find(
                      (operator) =>
                        operator.grn === definition.data?.operatorGrn,
                    )?.name
                  : undefined
              }
            />
          )}
        </>
      ),
    },
    {
      value: "execution",
      label: "Execution",
      content: (
        <Section className="rounded-xl border bg-card p-4" title="Execution">
          {hosted.status === "ok" ? (
            <ExecutionPanel
              org={org}
              agentGrn={agentGrn}
              hosted={hostedFor(
                hosted.data,
                "agent:execution-read",
                reported?.assignment?.assigneeGrn,
              )}
            />
          ) : (
            <ControlNotice
              operatorsHref={orgHref(org, "operators")}
              reason={hosted.reason}
              fix={NO_FIX}
            />
          )}
        </Section>
      ),
    },
  ];
  if (
    definition.data !== null &&
    accessDecisions(readings).get("rename") !== "denied"
  ) {
    tabs.push({
      value: "settings",
      label: "Settings",
      content: (
        <Section className="rounded-xl border bg-card p-4" title="Name">
          <RenameAgentForm
            org={org}
            agent={agent}
            name={definition.data.name}
          />
        </Section>
      ),
    });
  }

  return (
    <>
      <ItemCrumb label={name} />
      <DetailPage
        mark={<MemberMark kind="agent" name={name} className="size-7" />}
        name={name}
        idLabel="Identifier"
        id={agent}
        facts={[
          {
            term: "Runs on",
            description:
              state === "unchecked"
                ? "Not checked"
                : holder === null
                  ? "Nowhere yet"
                  : holder.kind === "operator"
                    ? holder.name
                    : `${holder.name} (standalone)`,
          },
          ...(reported?.generation === undefined
            ? []
            : [{ term: "Generation", description: reported.generation }]),
          {
            term: "Last report",
            description: reported ? (
              <Time value={reported.updatedAt} />
            ) : state === "unchecked" ? (
              "Not checked"
            ) : (
              "Never"
            ),
          },
          ...(definedBy === null
            ? []
            : [{ term: "Defined by", description: definedBy }]),
        ]}
        status={
          <span className="flex flex-wrap items-center gap-x-3 gap-y-1">
            <StatusDot {...dots.provisioning} />
            <StatusDot {...dots.runtime} />
          </span>
        }
        actions={
          <>
            <ReplaceHost
              org={org}
              agentGrn={agentGrn}
              agentName={name}
              holder={holder}
              assigneeGrn={reported?.assignment?.assigneeGrn}
              unchecked={state === "unchecked"}
              denied={accessDecisions(readings).get("enroll") === "denied"}
            />
            <Button asChild variant="outline">
              <Link
                href={`${orgHref(org, "permissions")}/attachments?subject=${encodeURIComponent(`agent:${agent}`)}`}
              >
                Manage policies
              </Link>
            </Button>
          </>
        }
        tabs={tabs}
      />
      <YourAccess readings={readings} />
    </>
  );
}

/**
 * Replacing the host an agent runs on (ADR-0082): for one a standalone
 * host holds, an approval for a new host naming the assignment the
 * header read, so garam refuses it if the agent has moved since. One an
 * operator holds is replaced through its control service (ADR-0107),
 * which the console does not reach, so the control stays, inactive,
 * with that reason (ADR-0113), as it does where nothing holds it or the
 * server denies it.
 */
function ReplaceHost({
  org,
  agentGrn,
  agentName,
  holder,
  assigneeGrn,
  unchecked,
  denied,
}: {
  org: string;
  agentGrn: string;
  agentName: string;
  holder: Holder | null;
  assigneeGrn: string | undefined;
  unchecked: boolean;
  denied: boolean;
}) {
  const reason = unchecked
    ? "What runs it is not shown to you."
    : holder === null
      ? "Nothing runs it yet."
      : holder.kind === "operator"
        ? `${holder.name}'s control service replaces this host.`
        : denied
          ? "No policy lets you replace this agent's host."
          : null;
  if (reason !== null || holder === null || assigneeGrn === undefined) {
    return (
      <InactiveButton reason={reason ?? "Not checked."}>
        Replace host
      </InactiveButton>
    );
  }
  return (
    <ApproveHostDialog
      org={org}
      replaces={{
        agentGrn,
        assigneeGrn,
        epoch: holder.epoch,
        agentName,
        hostName: holder.name,
      }}
      trigger={
        <Button type="button" variant="outline">
          Replace host
        </Button>
      }
    />
  );
}

/**
 * Why garam holds no definition for an agent it holds a report of,
 * said by what holds it: an operator's control service created it, or
 * a standalone host enrolled it (ADR-0082).
 */
function noDefinitionCause(holder: Holder | null): string {
  if (holder === null) return "No definition in garam.";
  return holder.kind === "operator"
    ? `Created by ${holder.name}'s control service.`
    : `Enrolled by the standalone host ${holder.name}.`;
}

/** What the organization defined for the agent (ADR-0075). */
function Requested({
  org,
  definition,
  operatorName,
}: {
  org: string;
  definition: AgentDefinition;
  operatorName: string | undefined;
}) {
  const { operatorGrn, values, createdAt } = definition;
  return (
    <Section className="rounded-xl border bg-card p-4" title="Definition">
      <DescriptionList
        items={[
          {
            term: "Operator",
            description: (
              <OperatorName
                org={org}
                identifier={slugFromGrn(operatorGrn)}
                name={operatorName}
              />
            ),
          },
          { term: "Defined", description: <Time value={createdAt} /> },
        ]}
      />
      <DesiredValues values={values} />
    </Section>
  );
}
