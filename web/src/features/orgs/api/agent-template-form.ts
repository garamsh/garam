import { z } from "zod";

import { parseTemplateFields, type TemplateFields } from "@/lib/agent-template";
import type { Parsed } from "@/lib/parsed";

/**
 * What the template form has to show: nothing yet, a refusal, or the
 * name an edit saved. Creating and editing share it because one form
 * does both; a created template is the page it is sent to, so creating
 * never answers `saved`.
 */
export type TemplateFormState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | { status: "saved"; name: string };

const Schema = z.object({
  org: z.string().min(1),
  name: z.string(),
  description: z.string(),
  entries: z.array(z.object({ key: z.string(), value: z.string() })),
});

/**
 * Reads what the template form submitted: the organization and the
 * template's fields. The editor submits one `key` and one `value` field
 * per row, so a submission whose two lists disagree leaves a row with a
 * half missing and the parse refuses it.
 */
export function readTemplateForm(
  formData: FormData,
): Parsed<{ org: string; fields: TemplateFields }> {
  const keyFields = formData.getAll("key");
  const valueFields = formData.getAll("value");
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    name: formData.get("name"),
    description: formData.get("description") ?? "",
    entries: Array.from(
      { length: Math.max(keyFields.length, valueFields.length) },
      (_, index) => ({ key: keyFields[index], value: valueFields[index] }),
    ),
  });
  if (!parsed.success) {
    return { status: "error", message: "The form did not arrive complete." };
  }
  const fields = parseTemplateFields(
    parsed.data.name,
    parsed.data.description,
    parsed.data.entries,
  );
  if (fields.status === "error") return fields;
  return {
    status: "ok",
    value: { org: parsed.data.org, fields: fields.value },
  };
}
