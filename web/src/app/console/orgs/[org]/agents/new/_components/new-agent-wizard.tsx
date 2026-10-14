"use client";

import Link from "next/link";
import { useState } from "react";

import { CopyField } from "@/components/copy-field";
import { Button } from "@/components/ui/button";

import { WizardPage } from "../../../../../_components/templates/wizard-page";
import {
  ControlNotice,
  type ControlReach,
} from "../../_components/control-notice";
import {
  CreateFromTemplate,
  identifierOf,
  type CreatedAgent,
} from "../../_components/create-from-template";

const STEPS = [
  {
    id: "service",
    title: "Control service",
    description: "Where the agent is created",
  },
  { id: "template", title: "Template", description: "What it starts from" },
  { id: "verify", title: "Verify", description: "The agent it created" },
] as const;

/**
 * Creating an agent, in the steps the console takes it: a control
 * service that can create one, the template it starts from, and the
 * agent it created. The first step's state is the server's, so a
 * visitor who fixed the operator and came back resumes at the template;
 * a blocked first step says why and links to the page that fixes it,
 * so the route is never a dead end.
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
  start: ControlReach;
  controllers: Readonly<Record<string, string>>;
}) {
  const [created, setCreated] = useState<CreatedAgent | null>(null);
  const current =
    start.status === "blocked"
      ? "service"
      : created === null
        ? "template"
        : "verify";
  const completed =
    current === "service"
      ? []
      : current === "template"
        ? ["service"]
        : ["service", "template"];
  const step = STEPS.find((s) => s.id === current);

  return (
    <WizardPage
      title="New agent"
      description="Create an agent from a template the control service published."
      steps={STEPS}
      current={current}
      completed={completed}
    >
      <div className="flex flex-col gap-4">
        <h2 className="text-base">{step?.title}</h2>
        {start.status === "blocked" ? (
          <>
            <p className="text-muted-foreground">
              garam no longer defines agents itself: a new agent is created by a
              hosted operator&apos;s control service, from a template it
              published.
            </p>
            <ControlNotice org={org} {...start.block} />
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
          <Created org={org} created={created} />
        )}
      </div>
    </WizardPage>
  );
}

/**
 * The agent the control service answered with. Its page reads its
 * execution back from the control service, which is what shows the
 * agent exists there.
 */
function Created({ org, created }: { org: string; created: CreatedAgent }) {
  const identifier = identifierOf(created.agent);
  return (
    <>
      <p role="status">
        Created at revision {created.revision}. It keeps the template version it
        copied.
      </p>
      <CopyField label="Agent identifier" value={identifier} />
      <div>
        <Button asChild>
          <Link href={`/console/orgs/${org}/agents/${identifier}`}>
            Open the agent
          </Link>
        </Button>
      </div>
    </>
  );
}
