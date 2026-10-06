"use client";

import { useState } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { mintOperationAuthority } from "@/features/orgs/actions";
import type { HostedOperator } from "@/features/orgs";
import {
  PublishedParser,
  callControl,
  segment,
  type TemplateVersion,
} from "@/lib/operation-authority";

import { UNAVAILABLE } from "./control-notice";

type Outcome =
  | { status: "idle" }
  | { status: "error"; message: string }
  | { status: "published"; name: string; version: number };

/** Reads `tool=pin` lines into the tool pins a configuration carries. */
function parseTools(text: string): Record<string, string> | null {
  const tools: Record<string, string> = {};
  for (const line of text.split("\n").map((l) => l.trim())) {
    if (line === "") continue;
    const at = line.indexOf("=");
    if (at <= 0) return null;
    tools[line.slice(0, at).trim()] = line.slice(at + 1).trim();
  }
  return tools;
}

/**
 * Publishes the next immutable version of a template (#1170), started
 * from the version on screen where one is. The control service numbers
 * it and validates it; garam only binds the request to the person
 * publishing. A secret is named by reference and never typed here.
 */
export function PublishTemplateForm({
  org,
  orgGrn,
  hosted,
  from,
  onPublished,
}: {
  org: string;
  orgGrn: string;
  hosted: HostedOperator;
  from: TemplateVersion | null;
  onPublished: () => void;
}) {
  const [outcome, setOutcome] = useState<Outcome>({ status: "idle" });
  const [pending, setPending] = useState(false);

  async function publish(form: FormData) {
    const field = (name: string) => String(form.get(name) ?? "").trim();
    const tools = parseTools(field("tools"));
    const version = Number(field("profileVersion"));
    if (
      field("name") === "" ||
      field("profileName") === "" ||
      !Number.isInteger(version) ||
      version < 1 ||
      tools === null
    ) {
      setOutcome({
        status: "error",
        message:
          "Name the template and a published profile version; write each tool as tool=pin.",
      });
      return;
    }
    // An embedding no field names is absent from the wire, not empty.
    const embedding = {
      baseUrl: field("embeddingBaseUrl"),
      name: field("embeddingModel"),
      apiKeyRef: field("embeddingApiKeyRef"),
    };
    const namesEmbedding = Object.values(embedding).some((v) => v !== "");
    setPending(true);
    const requestId = crypto.randomUUID();
    const result = await callControl(
      {
        endpoint: hosted.endpoint,
        path: `/v1/orgs/${segment(org)}/templates/${segment(field("name"))}/versions`,
        method: "POST",
        body: {
          requestId,
          profile: { name: field("profileName"), version },
          configuration: {
            model: {
              provider: field("provider"),
              baseUrl: field("baseUrl"),
              name: field("model"),
              apiKeyRef: field("apiKeyRef"),
              ...(namesEmbedding ? { embedding } : {}),
            },
            ego: field("ego"),
            tools,
          },
        },
        requestId,
        org,
        audience: hosted.grn,
        operation: "agent-template:publish",
        target: orgGrn,
        parser: PublishedParser,
      },
      mintOperationAuthority,
    );
    setPending(false);
    if (result.status === "ok") {
      setOutcome({ status: "published", ...result.data });
      onPublished();
    } else {
      setOutcome({
        status: "error",
        message: result.status === "unavailable" ? UNAVAILABLE : result.message,
      });
    }
  }

  const model = from?.configuration.model;
  return (
    <form action={publish} className="flex max-w-2xl flex-col gap-3">
      <h3 className="text-sm font-medium">Publish a template version</h3>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field
          id="publish-name"
          name="name"
          label="Template"
          value={from?.name}
        />
        <Field
          id="publish-profile"
          name="profileName"
          label="Profile"
          value={from?.profile.name}
        />
        <Field
          id="publish-profile-version"
          name="profileVersion"
          label="Profile version"
          value={from ? String(from.profile.version) : undefined}
        />
        <Field
          id="publish-provider"
          name="provider"
          label="Model provider"
          value={model?.provider}
        />
        <Field
          id="publish-base-url"
          name="baseUrl"
          label="Model base URL"
          value={model?.baseUrl}
        />
        <Field
          id="publish-model"
          name="model"
          label="Model name"
          value={model?.name}
        />
        <Field
          id="publish-api-key-ref"
          name="apiKeyRef"
          label="API key reference"
          value={model?.apiKeyRef}
        />
        <Field
          id="publish-embedding-base-url"
          name="embeddingBaseUrl"
          label="Embedding base URL"
          value={model?.embedding?.baseUrl}
        />
        <Field
          id="publish-embedding-model"
          name="embeddingModel"
          label="Embedding model name"
          value={model?.embedding?.name}
        />
        <Field
          id="publish-embedding-api-key-ref"
          name="embeddingApiKeyRef"
          label="Embedding API key reference"
          value={model?.embedding?.apiKeyRef}
        />
      </div>
      <div className="flex flex-col gap-2">
        <Label htmlFor="publish-ego">Ego</Label>
        <Textarea
          id="publish-ego"
          name="ego"
          rows={3}
          defaultValue={from?.configuration.ego}
        />
      </div>
      <div className="flex flex-col gap-2">
        <Label htmlFor="publish-tools">Tools, one tool=pin per line</Label>
        <Textarea
          id="publish-tools"
          name="tools"
          rows={3}
          defaultValue={Object.entries(from?.configuration.tools ?? {})
            .map(([tool, pin]) => `${tool}=${pin}`)
            .join("\n")}
        />
      </div>
      {outcome.status === "error" && (
        <p role="alert" className="text-sm text-destructive">
          {outcome.message}
        </p>
      )}
      {outcome.status === "published" && (
        <p role="status" className="text-sm text-muted-foreground">
          Published {outcome.name} version {outcome.version}. Agents created
          from earlier versions keep what they copied.
        </p>
      )}
      <div>
        <Button type="submit" variant="outline" disabled={pending}>
          {pending ? "Publishing…" : "Publish version"}
        </Button>
      </div>
    </form>
  );
}

function Field({
  id,
  name,
  label,
  value,
}: {
  id: string;
  name: string;
  label: string;
  value: string | undefined;
}) {
  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor={id}>{label}</Label>
      <Input id={id} name={name} defaultValue={value} />
    </div>
  );
}
