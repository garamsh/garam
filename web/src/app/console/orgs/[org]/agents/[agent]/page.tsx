import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { Banner } from "@/components/banner";
import { ControlNotice, NO_FIX } from "@/components/control-notice";
import { DescriptionList } from "@/components/description-list";
import { StageTrack } from "@/components/stage-track";
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
  slugFromGrn,
  type Agent,
  type AgentDefinition,
  type Reported as ReportedState,
} from "@/features/orgs";
import { checkPermissions } from "@/lib/permission-check";

import { ApiUnreachable } from "../../../../_components/api-unreachable";
import { CUTOVER_STAGE_LABEL } from "../../../../_components/cutover-marker";
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
import { reportedDots, type AgentDot } from "../_components/agent-status";
import { ExecutionPanel } from "../_components/execution-panel";
import { RenameAgentForm } from "./_components/rename-agent-form";

/** Head metadata for an agent's route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Agent — garam" };

type Params = { params: Promise<{ org: string; agent: string }> };

/** A cutover's stages in the order an agent passes them (ADR-0086). */
const CUTOVER_STAGES = [
  { id: "legacy", label: "Defined in garam" },
  { id: "frozen", label: CUTOVER_STAGE_LABEL.frozen },
  { id: "switched", label: CUTOVER_STAGE_LABEL.switched },
] as const;

/**
 * One agent: what the organization asked for where garam holds a
 * definition, what operators have reported (ADR-0075), and the
 * execution its hosted operator's control service holds (#1170), each
 * under a tab of its own, with the rename under Settings. An agent a
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
  const [organization, definition, agents, operators, hosted, permissions] =
    await Promise.all([
      getOrg(org),
      getAgentDefinition(org, agent),
      lookupAgents(org),
      lookupOperators(org),
      listHostedOperators(org),
      checkPermissions(org, {
        rename: {
          action: "agent:rename",
          resource: { type: "agent", id: agent },
        },
      }),
    ]);

  const fallback = (
    <PageHeader title="Agent" description="An agent of this organization." />
  );
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

  const tabs: DetailTab[] = [
    {
      value: "overview",
      label: "Overview",
      content: (
        <>
          <Reported state={state} dots={dots} agent={reported} />
          {definition.data === null ? (
            <Banner tone="info" title="No definition in garam">
              A control service created this agent. garam holds what its
              operator reports, and no definition.
            </Banner>
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
        <Section
          className="rounded-xl border bg-card p-4"
          title="Execution"
          description="What the hosted operator's control service holds for this agent."
        >
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
  if (definition.data !== null && permissions.get("rename") !== "denied") {
    tabs.push({
      value: "settings",
      label: "Settings",
      content: (
        <Section
          className="rounded-xl border bg-card p-4"
          title="Name"
          description="The organization's word for this agent. Its identifier, operator and values stay as they are."
        >
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
        name={name}
        idLabel="Agent identifier"
        id={agent}
        status={
          <span className="flex flex-wrap items-center gap-x-3 gap-y-1">
            <StatusDot {...dots.provisioning} />
            <StatusDot {...dots.runtime} />
          </span>
        }
        actions={
          <Button asChild variant="outline">
            <Link
              href={`${orgHref(org, "permissions")}/attachments?subject=${encodeURIComponent(`agent:${agent}`)}`}
            >
              Manage policies
            </Link>
          </Button>
        }
        tabs={tabs}
      />
    </>
  );
}

/**
 * What operators and the agent itself reported, apart from what the
 * organization asked for (ADR-0053). `Last report` is when one last
 * landed — nothing expires one.
 */
function Reported({
  state,
  dots,
  agent,
}: {
  state: ReportedState;
  dots: { provisioning: AgentDot; runtime: AgentDot };
  agent: Agent | undefined;
}) {
  return (
    <Section
      className="rounded-xl border bg-card p-4"
      title="Reported"
      description="What the operator and the agent itself last reported."
    >
      <DescriptionList
        items={[
          {
            term: "Provisioning",
            description: <StatusDot {...dots.provisioning} />,
          },
          { term: "Runtime", description: <StatusDot {...dots.runtime} /> },
          {
            term: "Last report",
            description:
              // A refused list says nothing about whether a report
              // landed, so it is not checked rather than none.
              state === "unchecked" ? (
                <span className="text-muted-foreground">Not checked</span>
              ) : agent === undefined ? (
                <span className="text-muted-foreground">None</span>
              ) : (
                <Time value={agent.updatedAt} />
              ),
          },
        ]}
      />
    </Section>
  );
}

/**
 * What the organization asked for. A definition whose agent is cutting
 * over or cut over to the Operator keeps its values as an archive no
 * one edits, and only its name still changes (ADR-0086); where its
 * cutover stands is shown as a track with no control on it.
 */
function Requested({
  org,
  definition,
  operatorName,
}: {
  org: string;
  definition: AgentDefinition;
  operatorName: string | undefined;
}) {
  const { operatorGrn, values, createdAt, cutover } = definition;
  return (
    <Section
      className="rounded-xl border bg-card p-4"
      title="Requested"
      description="What this organization defined for the agent."
    >
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
      {cutover !== null && (
        <>
          <StageTrack
            label="Cut-over"
            stages={CUTOVER_STAGES}
            current={cutover.stage}
          />
          <Banner
            tone="info"
            title="These settings are archival and read-only in garam"
          >
            {CUTOVER_STAGE_LABEL[cutover.stage]}. Only the name still changes
            here.
          </Banner>
        </>
      )}
      <h3 className="font-medium">Values</h3>
      <DesiredValues values={values} />
      {cutover === null && (
        <p className="text-muted-foreground">
          Saved values; application is not reported.
        </p>
      )}
    </Section>
  );
}
