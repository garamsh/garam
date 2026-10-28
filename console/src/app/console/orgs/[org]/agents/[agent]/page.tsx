import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { ControlNotice, NO_FIX } from "@/components/control-notice";
import { MemberMark } from "@/components/member-mark";
import { StatusDot } from "@/components/status-dot";
import { Time } from "@/components/time";
import { Button } from "@garam/design/ui/button";
import {
  agentDisplayName,
  getOrg,
  hostedFor,
  listHostedOperators,
  lookupAgents,
  lookupOperators,
  slugFromGrn,
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
 * (#1576), and the execution its hosted operator's control service
 * holds (#1170). garam holds no definition of an agent (ADR-0118), so
 * the agents list is where one is found, and an identifier it does not
 * name answers 404. The organization is read for itself, so one the
 * caller cannot read answers 404 (ADR-0011); inside it, a refused
 * agents list says so. A control service that is missing or unavailable
 * disables only the Execution tab.
 */
export default async function AgentPage({ params }: Params) {
  const { org, agent } = await params;
  const self = { type: "agent", id: agent } as const;
  // Started before the page's own reads, so it runs beside them.
  const access = readAccess(org, [
    {
      key: "enroll",
      label: "Replace its standalone host",
      check: { action: "agent:enroll", resource: self },
    },
    {
      key: "rename",
      label: "Rename it",
      check: { action: "agent:rename", resource: self },
    },
  ]);
  const [organization, agents, operators, hosted, readings] = await Promise.all(
    [
      getOrg(org),
      lookupAgents(org),
      lookupOperators(org),
      listHostedOperators(org),
      access,
    ],
  );

  const fallback = <PageHeader title="Agent" />;
  if (
    organization.status === "unreachable" ||
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
  if (agents.status === "forbidden") {
    return (
      <>
        {fallback}
        <RefusedRead subject="this agent" />
      </>
    );
  }

  const reported = agents.data.find(
    (candidate) => slugFromGrn(candidate.grn) === agent,
  );
  if (reported === undefined) notFound();
  const agentGrn = reported.grn;
  // What the organization calls it, or its identifier where it calls
  // it nothing (ADR-0119).
  const name = agentDisplayName({
    name: reported.name ?? null,
    identifier: agent,
  });
  const decisions = accessDecisions(readings);
  const dots = reportedDots("reported", reported);
  const holder = holderOf(
    reported,
    new Map(
      (operators.status === "ok" ? operators.data : []).map((operator) => [
        operator.grn,
        operator.name,
      ]),
    ),
  );

  const tabs: DetailTab[] = [
    {
      value: "overview",
      label: "Overview",
      content: (
        <div className="flex flex-col gap-6">
          <p className="text-muted-foreground">{origin(holder)}</p>
          <Section title="Name">
            {decisions.get("rename") === "denied" ? (
              <p className="text-muted-foreground">
                No policy lets you rename this agent.
              </p>
            ) : (
              <RenameAgentForm
                org={org}
                agent={agent}
                name={reported.name ?? null}
              />
            )}
          </Section>
        </div>
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
                reported.assignment?.assigneeGrn,
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
              holder === null
                ? "Nowhere yet"
                : holder.kind === "operator"
                  ? holder.name
                  : `${holder.name} (standalone)`,
          },
          ...(reported.generation === undefined
            ? []
            : [{ term: "Generation", description: reported.generation }]),
          {
            term: "Last report",
            description: <Time value={reported.updatedAt} />,
          },
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
              assigneeGrn={reported.assignment?.assigneeGrn}
              denied={decisions.get("enroll") === "denied"}
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
  denied,
}: {
  org: string;
  agentGrn: string;
  agentName: string;
  holder: Holder | null;
  assigneeGrn: string | undefined;
  denied: boolean;
}) {
  const reason =
    holder === null
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
 * Where an agent garam holds a report of came from, said by what holds
 * it: an operator's control service created it, or a standalone host
 * enrolled it (ADR-0082).
 */
function origin(holder: Holder | null): string {
  if (holder === null) return "Nothing holds it yet.";
  return holder.kind === "operator"
    ? `Created by ${holder.name}'s control service.`
    : `Enrolled by the standalone host ${holder.name}.`;
}
