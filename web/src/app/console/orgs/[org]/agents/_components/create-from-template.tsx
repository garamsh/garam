"use client";

import Link from "next/link";
import { useState } from "react";

import { KeptSelect } from "@/components/kept-select";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { mintOperationAuthority } from "@/features/orgs/actions";
import type { HostedOperator } from "@/features/orgs";
import {
  CreatedParser,
  TemplatesParser,
  callControl,
  segment,
} from "@/lib/operation-authority";

import { ControlNotice, UNAVAILABLE, controlBlocker } from "./control-notice";
import { useControlRead } from "./use-control-read";

type Outcome =
  | { status: "idle" }
  | { status: "error"; message: string }
  | { status: "created"; agent: string; revision: string };

/**
 * Creates an agent from a template version the hosted operator's
 * control service published (#1170). The control service copies that
 * version into the agent's first revision once; publishing a later
 * version never reaches it. Creating needs `agent:create` and the
 * template list `agent-template:read`, and neither grants the other.
 */
export function CreateFromTemplate({
  org,
  orgGrn,
  hosted,
  controllers,
}: {
  org: string;
  orgGrn: string;
  hosted: HostedOperator | null;
  /** Names for the controllers the delegation lists. */
  controllers: Readonly<Record<string, string>>;
}) {
  const [outcome, setOutcome] = useState<Outcome>({ status: "idle" });
  const [pending, setPending] = useState(false);
  // Null is the first option, which the lists below are not read in
  // time to name.
  const [templateChoice, setTemplateChoice] = useState<string | null>(null);
  const [controllerChoice, setControllerChoice] = useState<string | null>(null);
  const blocker =
    controlBlocker(hosted, "agent:create") ??
    controlBlocker(hosted, "agent-template:read");
  const templates = useControlRead(
    org,
    "agent-template:read",
    hosted !== null && blocker === null
      ? {
          endpoint: hosted.endpoint,
          audience: hosted.grn,
          path: `/v1/orgs/${segment(org)}/templates`,
          target: orgGrn,
        }
      : null,
    TemplatesParser,
  );

  if (blocker !== null || hosted === null || hosted.delegation === null) {
    return <ControlNotice>{blocker}</ControlNotice>;
  }
  if (templates === null || templates.status === "loading") {
    return (
      <ControlNotice>Reading {hosted.name}&apos;s templates…</ControlNotice>
    );
  }
  if (templates.status !== "ok") {
    return (
      <ControlNotice>
        {templates.status === "unavailable" ? UNAVAILABLE : templates.message}
      </ControlNotice>
    );
  }
  const published = templates.data.templates;
  const delegated = hosted.delegation.controllers;

  async function create(form: FormData) {
    const chosen = published.find((t) => t.name === form.get("template"));
    const controller = String(form.get("controller") ?? "");
    if (!chosen || !delegated.includes(controller) || hosted === null) {
      setOutcome({
        status: "error",
        message: "Choose a template and a controller.",
      });
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
    setOutcome(
      result.status === "ok"
        ? { status: "created", ...result.data }
        : {
            status: "error",
            message:
              result.status === "unavailable" ? UNAVAILABLE : result.message,
          },
    );
  }

  if (outcome.status === "created") {
    const identifier = outcome.agent.split(":").at(-1) ?? outcome.agent;
    return (
      <p role="status" className="text-sm">
        Created{" "}
        <Link
          href={`/console/orgs/${org}/agents/${identifier}`}
          className="font-mono underline-offset-4 hover:underline"
        >
          {identifier}
        </Link>{" "}
        at revision {outcome.revision}. It keeps the template version it copied.
      </p>
    );
  }
  return (
    <form action={create} className="flex max-w-2xl flex-col gap-3">
      <div className="flex flex-col gap-2">
        <Label htmlFor="create-template">Template</Label>
        <KeptSelect
          id="create-template"
          name="template"
          value={templateChoice ?? published[0]?.name ?? ""}
          onChange={(event) => setTemplateChoice(event.target.value)}
          className="h-9 max-w-sm rounded-md border bg-transparent px-3 text-sm"
        >
          {published.map((t) => (
            <option key={t.name} value={t.name}>
              {t.name} version {t.version}
            </option>
          ))}
        </KeptSelect>
      </div>
      <div className="flex flex-col gap-2">
        <Label htmlFor="create-controller">Controller</Label>
        <KeptSelect
          id="create-controller"
          name="controller"
          value={controllerChoice ?? delegated[0] ?? ""}
          onChange={(event) => setControllerChoice(event.target.value)}
          className="h-9 max-w-sm rounded-md border bg-transparent px-3 text-sm"
        >
          {delegated.map((c) => (
            <option key={c} value={c}>
              {controllers[c] ?? c}
            </option>
          ))}
        </KeptSelect>
      </div>
      {outcome.status === "error" && (
        <p role="alert" className="text-sm text-destructive">
          {outcome.message}
        </p>
      )}
      <div>
        <Button type="submit" disabled={pending || published.length === 0}>
          {pending ? "Creating…" : "Create from template"}
        </Button>
      </div>
    </form>
  );
}
