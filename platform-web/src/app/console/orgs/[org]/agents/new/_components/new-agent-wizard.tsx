"use client";

import Link from "next/link";
import { useState } from "react";

import {
  ControlNotice,
  controlReach,
  type ControlReach,
} from "@/components/control-notice";
import type { DelegatedOperator, HostedOperator } from "@/features/orgs";
import { CopyField } from "@/components/copy-field";
import { Button } from "@garam/design/ui/button";
import { parseGrn } from "@/lib/grn";

import { orgHref } from "../../../../../_components/navigation";
import { WizardPage } from "../../../../../_components/templates/wizard-page";
import {
  CreateFromTemplate,
  type CreatedAgent,
} from "../../_components/create-from-template";
import { ExecutionPanel } from "../../_components/execution-panel";

/**
 * The last step verifies only where the grant lets the console read the
 * agent's execution back; otherwise it shows what was created and is
 * named for that.
 */
function steps(verifies: boolean) {
  return [
    { id: "service", title: "Control service" },
    { id: "template", title: "Template" },
    verifies
      ? { id: "last", title: "Verify" }
      : { id: "last", title: "Created" },
  ];
}

/**
 * Creating an agent, in the steps the console takes it: a control
 * service that can create one, the template it starts from, and the
 * agent it created, read back from the control service where the grant
 * allows. The first step's state is the server's, so a visitor who fixed
 * the operator and came back resumes at the template; a blocked first
 * step says why and links to the page that fixes it, so the route is
 * never a dead end.
 */
export function NewAgentWizard({
  org,
  orgGrn,
  start,
  controllers,
}: {
  org: string;
  orgGrn: string;
  /** Whether a control service can create, as the server read it. */
  start: ControlReach<HostedOperator>;
  controllers: Readonly<Record<string, string>>;
}) {
  const [created, setCreated] = useState<CreatedAgent | null>(null);
  const verifies =
    start.status === "ready" &&
    controlReach(start.hosted, "agent:execution-read").status === "ready";
  const wizardSteps = steps(verifies);
  const current =
    start.status === "blocked"
      ? "service"
      : created === null
        ? "template"
        : "last";
  const completed =
    current === "service"
      ? []
      : current === "template"
        ? ["service"]
        : ["service", "template"];
  const step = wizardSteps.find((s) => s.id === current);

  return (
    <WizardPage
      title="New agent"
      steps={wizardSteps}
      current={current}
      completed={completed}
    >
      <div className="flex flex-col gap-4">
        <h2 className="text-base">{step?.title}</h2>
        {start.status === "blocked" ? (
          <>
            <ControlNotice
              operatorsHref={orgHref(org, "operators")}
              {...start.block}
            />
          </>
        ) : created === null ? (
          <>
            <p className="text-muted-foreground">
              {start.hosted.name}&apos;s control service creates the agent.
            </p>
            <CreateFromTemplate
              org={org}
              orgGrn={orgGrn}
              hosted={start.hosted}
              controllers={controllers}
              onCreated={setCreated}
            />
          </>
        ) : (
          <Created
            org={org}
            created={created}
            readBack={verifies ? start.hosted : null}
          />
        )}
      </div>
    </WizardPage>
  );
}

/**
 * The agent the control service answered with and, where `readBack`
 * may read it, its execution as the control service now holds it —
 * which is what shows the agent exists there.
 */
function Created({
  org,
  created,
  readBack,
}: {
  org: string;
  created: CreatedAgent;
  readBack: DelegatedOperator | null;
}) {
  const identifier = parseGrn(created.agent)?.identifier ?? null;
  return (
    <>
      <p role="status">Created at revision {created.revision}.</p>
      {readBack !== null && (
        <ExecutionPanel org={org} agentGrn={created.agent} hosted={readBack} />
      )}
      {identifier !== null && (
        <>
          <CopyField label="Identifier" value={identifier} />
          <div>
            <Button asChild>
              <Link href={`/console/orgs/${org}/agents/${identifier}`}>
                Open the agent
              </Link>
            </Button>
          </div>
        </>
      )}
    </>
  );
}
