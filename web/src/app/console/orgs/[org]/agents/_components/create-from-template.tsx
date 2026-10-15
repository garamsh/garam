"use client";

import Link from "next/link";
import { useState } from "react";

import { Banner } from "@/components/banner";
import { ControlNotice, NO_FIX } from "@/components/control-notice";
import { FormField } from "@/components/form-field";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { DelegatedOperator } from "@/features/orgs";
import { useControlRead } from "@/hooks/use-control-read";
import { slugFromGrn } from "@/lib/grn";
import { controlFailureCopy } from "@/lib/control-refusal-copy";
import { mintOperationAuthority } from "@/features/orgs/actions";
import {
  CreatedParser,
  TemplatesParser,
  callControl,
  segment,
} from "@/lib/operation-authority";

import { orgHref } from "../../../../_components/navigation";

/** The agent a control service created, as its answer named it. */
export type CreatedAgent = { agent: string; revision: string };

type Outcome =
  | { status: "idle" }
  | { status: "invalid"; errors: { template?: string; controller?: string } }
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
  // Each starts unchosen: Radix resets a select in a form to the value
  // it mounted with, and only an empty one is a reset the handlers
  // below can tell from a choice.
  const [template, setTemplate] = useState("");
  const [controller, setController] = useState("");
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
    mintOperationAuthority,
  );

  const delegated = hosted.delegation.controllers;
  if (delegated.length === 0) {
    return (
      <ControlNotice
        operatorsHref={orgHref(org, "operators")}
        reason={`${hosted.name}'s delegation names no controller, so there is no operator to place an agent on.`}
        fix={{ at: "operator", identifier: hosted.identifier }}
      />
    );
  }
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
        operatorsHref={orgHref(org, "operators")}
        reason={controlFailureCopy(templates, "agent-template:read")}
        fix={NO_FIX}
      />
    );
  }
  const published = templates.data.templates;
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
    const placedOn = String(form.get("controller") ?? "");
    // Each choice is checked on its own, so the one that is wrong is
    // the one flagged.
    const errors = {
      ...(chosen === undefined ? { template: "Choose a template." } : {}),
      ...(!delegated.includes(placedOn)
        ? { controller: "Choose a controller." }
        : {}),
    };
    if (chosen === undefined || Object.keys(errors).length > 0) {
      setOutcome({ status: "invalid", errors });
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
          controller: placedOn,
          template: { name: chosen.name, version: chosen.version },
          profile: chosen.profile,
        },
        requestId,
        org,
        audience: hosted.grn,
        operation: "agent:create",
        target: placedOn,
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
      message: controlFailureCopy(result, "agent:create"),
    });
  }

  const errors = outcome.status === "invalid" ? outcome.errors : {};
  return (
    <form action={create} className="flex max-w-md flex-col gap-4">
      <input type="hidden" name="template" value={template} />
      <input type="hidden" name="controller" value={controller} />
      <FormField
        label="Template"
        hint="The version is copied once; a later version never changes the agent."
        error={errors.template}
      >
        {(control) => (
          <Select
            value={template}
            onValueChange={(value) => {
              // Radix answers the form's reset after every action with
              // the empty default; no option is empty, so that is not
              // a choice, and a refused one stays chosen.
              if (value === "") return;
              setTemplate(value);
            }}
          >
            <SelectTrigger {...control} className="w-full min-w-0">
              <SelectValue placeholder="Choose a template" />
            </SelectTrigger>
            <SelectContent position="popper">
              {published.map((t) => (
                <SelectItem key={t.name} value={t.name}>
                  {t.name} version {t.version}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
      </FormField>
      <FormField
        label="Controller"
        hint={`The operators ${hosted.name}'s delegation lets it place an agent on.`}
        error={errors.controller}
      >
        {(control) => (
          <Select
            value={controller}
            onValueChange={(value) => {
              if (value === "") return;
              setController(value);
            }}
          >
            <SelectTrigger {...control} className="w-full min-w-0">
              <SelectValue placeholder="Choose a controller" />
            </SelectTrigger>
            <SelectContent position="popper">
              {delegated.map((c) => (
                <SelectItem key={c} value={c}>
                  {controllers[c] ?? slugFromGrn(c)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
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
