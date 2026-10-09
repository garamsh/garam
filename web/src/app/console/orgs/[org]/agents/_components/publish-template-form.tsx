"use client";

import { useState } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { mintOperationAuthority } from "@/features/orgs/actions";
import type { HostedOperator } from "@/features/orgs";
import { useHydrated } from "@/hooks/use-hydrated";
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

/** What each field holds, by the name it submits under. */
type Values = {
  name: string;
  profileName: string;
  profileVersion: string;
  provider: string;
  baseUrl: string;
  model: string;
  apiKeyRef: string;
  embeddingBaseUrl: string;
  embeddingModel: string;
  embeddingApiKeyRef: string;
  ego: string;
  tools: string;
};

/** The fields as the version on screen fills them, or empty. */
function startingValues(from: TemplateVersion | null): Values {
  const model = from?.configuration.model;
  return {
    name: from?.name ?? "",
    profileName: from?.profile.name ?? "",
    profileVersion: from ? String(from.profile.version) : "",
    provider: model?.provider ?? "",
    baseUrl: model?.baseUrl ?? "",
    model: model?.name ?? "",
    apiKeyRef: model?.apiKeyRef ?? "",
    embeddingBaseUrl: model?.embedding?.baseUrl ?? "",
    embeddingModel: model?.embedding?.name ?? "",
    embeddingApiKeyRef: model?.embedding?.apiKeyRef ?? "",
    ego: from?.configuration.ego ?? "",
    tools: Object.entries(from?.configuration.tools ?? {})
      .map(([tool, pin]) => `${tool}=${pin}`)
      .join("\n"),
  };
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
  const hydrated = useHydrated();
  // Controlled, so a refused publish keeps what was typed: a form action
  // resets an uncontrolled field when it completes, refusal included.
  const [values, setValues] = useState(() => startingValues(from));
  const control = (name: keyof Values) => ({
    name,
    value: values[name],
    disabled: !hydrated,
    onChange: (
      event: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>,
    ) => setValues((held) => ({ ...held, [name]: event.target.value })),
  });

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

  return (
    <form action={publish} className="flex max-w-2xl flex-col gap-3">
      <h3 className="text-sm font-medium">Publish a template version</h3>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field id="publish-name" label="Template" {...control("name")} />
        <Field
          id="publish-profile"
          label="Profile"
          {...control("profileName")}
        />
        <Field
          id="publish-profile-version"
          label="Profile version"
          {...control("profileVersion")}
        />
        <Field
          id="publish-provider"
          label="Model provider"
          {...control("provider")}
        />
        <Field
          id="publish-base-url"
          label="Model base URL"
          {...control("baseUrl")}
        />
        <Field id="publish-model" label="Model name" {...control("model")} />
        <Field
          id="publish-api-key-ref"
          label="API key reference"
          {...control("apiKeyRef")}
        />
        <Field
          id="publish-embedding-base-url"
          label="Embedding base URL"
          {...control("embeddingBaseUrl")}
        />
        <Field
          id="publish-embedding-model"
          label="Embedding model name"
          {...control("embeddingModel")}
        />
        <Field
          id="publish-embedding-api-key-ref"
          label="Embedding API key reference"
          {...control("embeddingApiKeyRef")}
        />
      </div>
      <div className="flex flex-col gap-2">
        <Label htmlFor="publish-ego">Ego</Label>
        <Textarea id="publish-ego" rows={3} {...control("ego")} />
      </div>
      <div className="flex flex-col gap-2">
        <Label htmlFor="publish-tools">Tools, one tool=pin per line</Label>
        <Textarea id="publish-tools" rows={3} {...control("tools")} />
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
        <Button type="submit" variant="outline" disabled={!hydrated || pending}>
          {pending ? "Publishing…" : "Publish version"}
        </Button>
      </div>
    </form>
  );
}

function Field({
  id,
  label,
  ...input
}: {
  id: string;
  label: string;
} & React.ComponentProps<typeof Input>) {
  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor={id}>{label}</Label>
      <Input id={id} {...input} />
    </div>
  );
}
