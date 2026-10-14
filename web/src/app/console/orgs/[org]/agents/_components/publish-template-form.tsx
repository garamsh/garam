"use client";

import { useState } from "react";

import { FormField } from "@/components/form-field";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { mintOperationAuthority } from "@/features/orgs/actions";
import { useHydrated } from "@/hooks/use-hydrated";
import {
  PublishedParser,
  callControl,
  segment,
  type TemplateVersion,
} from "@/lib/operation-authority";

import { UNAVAILABLE, type DelegatedOperator } from "./control-notice";

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

/** The sentence each field the form checks shows when it is wrong. */
type FieldErrors = Partial<Record<keyof Values, string>>;

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

/** The labelled single-line fields, in the order the form shows them. */
const INPUTS: readonly { name: keyof Values; label: string }[] = [
  { name: "name", label: "Template" },
  { name: "profileName", label: "Profile" },
  { name: "profileVersion", label: "Profile version" },
  { name: "provider", label: "Model provider" },
  { name: "baseUrl", label: "Model base URL" },
  { name: "model", label: "Model name" },
  { name: "apiKeyRef", label: "API key reference" },
  { name: "embeddingBaseUrl", label: "Embedding base URL" },
  { name: "embeddingModel", label: "Embedding model name" },
  { name: "embeddingApiKeyRef", label: "Embedding API key reference" },
];

/**
 * Publishes the next immutable version of a template (#1170), started
 * from the version on screen where one is. The control service numbers
 * it and validates it; garam only binds the request to the person
 * publishing. A secret is named by reference and never typed here. A
 * field the form can check carries its own error; a refusal from the
 * control service is said beside Publish.
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
  hosted: DelegatedOperator;
  from: TemplateVersion | null;
  onPublished: (published: { name: string; version: number }) => void;
}) {
  const [errors, setErrors] = useState<FieldErrors>({});
  const [refusal, setRefusal] = useState<string | null>(null);
  const [pending, setPending] = useState(false);
  const hydrated = useHydrated();
  // Controlled, so a refused publish keeps what was typed: a form
  // action resets an uncontrolled field when it completes, refusal
  // included.
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
    const field = (name: keyof Values) => String(form.get(name) ?? "").trim();
    const tools = parseTools(field("tools"));
    const version = Number(field("profileVersion"));
    const invalid: FieldErrors = {
      ...(field("name") === "" ? { name: "Name the template." } : {}),
      ...(field("profileName") === ""
        ? { profileName: "Name a published profile." }
        : {}),
      ...(!Number.isInteger(version) || version < 1
        ? { profileVersion: "Give the profile's version as a whole number." }
        : {}),
      ...(tools === null ? { tools: "Write each tool as tool=pin." } : {}),
    };
    setErrors(invalid);
    setRefusal(null);
    if (Object.keys(invalid).length > 0 || tools === null) return;
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
      onPublished(result.data);
    } else {
      setRefusal(
        result.status === "unavailable" ? UNAVAILABLE : result.message,
      );
    }
  }

  return (
    <form action={publish} className="flex flex-col gap-4">
      <div className="grid gap-4 sm:grid-cols-2">
        {INPUTS.map(({ name, label }) => (
          <FormField key={name} label={label} error={errors[name]}>
            {(props) => <Input {...props} {...control(name)} />}
          </FormField>
        ))}
      </div>
      <FormField label="Ego">
        {(props) => <Textarea {...props} rows={3} {...control("ego")} />}
      </FormField>
      <FormField
        label="Tools"
        hint="One tool=pin per line."
        error={errors.tools}
      >
        {(props) => <Textarea {...props} rows={3} {...control("tools")} />}
      </FormField>
      <div className="flex flex-wrap items-center gap-3">
        <Button type="submit" disabled={!hydrated || pending}>
          {pending ? "Publishing…" : "Publish version"}
        </Button>
        {refusal !== null && (
          <p role="alert" className="text-danger">
            {refusal}
          </p>
        )}
      </div>
    </form>
  );
}
