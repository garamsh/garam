import { refuse, type Parsed } from "@/lib/parsed";

/**
 * A policy document as the console holds one: a JSON object. The
 * engine owns the model and validates every key on write
 * (`api/openapi.yaml` `PolicyDocument`), so the console checks only
 * that the text is an object and leaves the rest to the engine's
 * refusal. The generated type is `Record<string, never>`, since the
 * specification declares an object with no properties of its own.
 */
export type PolicyDocument = { [key: string]: unknown };

/**
 * The document typed into a form, or the sentence shown in place of
 * it. Applied by the form before submitting and by the Server Action
 * at the boundary.
 */
export function parsePolicyDocument(text: string): Parsed<PolicyDocument> {
  let value: unknown;
  try {
    value = JSON.parse(text);
  } catch {
    return refuse("The document is not valid JSON.");
  }
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return refuse("The document must be a JSON object.");
  }
  return { status: "ok", value: { ...value } };
}

/** A document as the editor shows it: indented JSON. */
export function formatPolicyDocument(document: PolicyDocument): string {
  return JSON.stringify(document, null, 2);
}
