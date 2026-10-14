"use client";

import Link from "next/link";
import { useState } from "react";

import { Banner } from "@/components/banner";
import { FormField } from "@/components/form-field";
import { KeptSelect } from "@/components/kept-select";
import { Button } from "@/components/ui/button";
import { mintOperationAuthority } from "@/features/orgs/actions";
import {
  CreatedParser,
  TemplatesParser,
  callControl,
  segment,
} from "@/lib/operation-authority";

import {
  ControlNotice,
  UNAVAILABLE,
  type DelegatedOperator,
} from "./control-notice";
import { useControlRead } from "./use-control-read";

/** The last segment of a GRN, which the console shows in its place. */
export function identifierOf(grn: string): string {
  return grn.split(":").at(-1) ?? grn;
}

/** The agent a control service created, as its answer named it. */
export type CreatedAgent = { agent: string; revision: string };

type Outcome =
  | { status: "idle" }
  | { status: "invalid" }
  | { status: "refused"; message: string };

/**
 * Creates an agent from a template version the hosted operator's
 * control service published (#1170). The control service copies that
 * version into the agent's first revision once; publishing a later
 * version never reaches it. Creating needs `agent:create` and the
 * template list `agent-template:read`, and neither grants the other;
 * the caller has already checked the grant carries both.
 */
export function CreateFromTemplate({
  org,
  orgGrn,
  hosted,
  controllers,
  onCreated,
}: {
  org: string;
  orgGrn: string;
  hosted: DelegatedOperator;
  /** Names for the controllers the delegation lists. */
  controllers: Readonly<Record<string, string>>;
  onCreated: (created: CreatedAgent) => void;
}) {
  const [outcome, setOutcome] = useState<Outcome>({ status: "idle" });
  const [pending, setPending] = useState(false);
  // Null is the first option, which the lists below are not read in
  // time to name.
  const [templateChoice, setTemplateChoice] = useState<string | null>(null);
  const [controllerChoice, setControllerChoice] = useState<string | null>(null);
  const templates = useControlRead(
    org,
    "agent-template:read",
    {
      endpoint: hosted.endpoint,
      audience: hosted.grn,
      path: `/v1/orgs/${segment(org)}/templates`,
      target: orgGrn,
    },
    TemplatesParser,
  );

  if (templates === null || templates.status === "loading") {
    return (
      <p role="status" className="text-muted-foreground">
        Reading {hosted.name}&apos;s templates…
      </p>
    );
  }
  if (templates.status !== "ok") {
    return (
      <ControlNotice
        org={org}
        reason={
          templates.status === "unavailable" ? UNAVAILABLE : templates.message
        }
      />
    );
  }
  const published = templates.data.templates;
  const delegated = hosted.delegation.controllers;
  if (published.length === 0) {
    return (
      <Banner
        tone="info"
        title="No template is published yet"
        action={
          <Button asChild variant="outline" size="sm">
            <Link href={`/console/orgs/${org}/agents/templates`}>
              Open templates
            </Link>
          </Button>
        }
      >
        An agent starts from a template version {hosted.name} published. Publish
        one under Templates, then come back here.
      </Banner>
    );
  }

  async function create(form: FormData) {
    const chosen = published.find((t) => t.name === form.get("template"));
    const controller = String(form.get("controller") ?? "");
    if (!chosen || !delegated.includes(controller)) {
      setOutcome({ status: "invalid" });
      return;
    }
    setPending(true);
    const requestId = crypto.randomUUID();
    const result = await callControl(
      {
        endpoint: hosted.endpoint,
        path: `/v1/orgs/${segment(org)}/agents`,
        method: "POST",
        body: {
          requestId,
          controller,
          template: { name: chosen.name, version: chosen.version },
          profile: chosen.profile,
        },
        requestId,
        org,
        audience: hosted.grn,
        operation: "agent:create",
        target: controller,
        parser: CreatedParser,
      },
      mintOperationAuthority,
    );
    setPending(false);
    if (result.status === "ok") {
      onCreated(result.data);
      return;
    }
    setOutcome({
      status: "refused",
      message: result.status === "unavailable" ? UNAVAILABLE : result.message,
    });
  }

  return (
    <form action={create} className="flex max-w-md flex-col gap-4">
      <FormField
        label="Template"
        hint="The version is copied once; a later version never changes the agent."
        error={outcome.status === "invalid" ? "Choose a template." : undefined}
      >
        {(control) => (
          <KeptSelect
            {...control}
            name="template"
            value={templateChoice ?? published[0]?.name ?? ""}
            onChange={(event) => setTemplateChoice(event.target.value)}
          >
            {published.map((t) => (
              <option key={t.name} value={t.name}>
                {t.name} version {t.version}
              </option>
            ))}
          </KeptSelect>
        )}
      </FormField>
      <FormField
        label="Controller"
        hint={`The operators ${hosted.name}'s delegation lets it place an agent on.`}
        error={
          outcome.status === "invalid" ? "Choose a controller." : undefined
        }
      >
        {(control) => (
          <KeptSelect
            {...control}
            name="controller"
            value={controllerChoice ?? delegated[0] ?? ""}
            onChange={(event) => setControllerChoice(event.target.value)}
          >
            {delegated.map((c) => (
              <option key={c} value={c}>
                {controllers[c] ?? identifierOf(c)}
              </option>
            ))}
          </KeptSelect>
        )}
      </FormField>
      <div className="flex flex-wrap items-center gap-3">
        <Button type="submit" disabled={pending}>
          {pending ? "Creating…" : "Create from template"}
        </Button>
        {outcome.status === "refused" && (
          <p role="alert" className="text-danger">
            {outcome.message}
          </p>
        )}
      </div>
    </form>
  );
}
