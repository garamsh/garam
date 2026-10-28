import { z } from "zod";

import { pointerTokens } from "@/lib/json-pointer";
import { refuse, type Parsed } from "@garam/platform-client/parsed";

/**
 * A policy document as the console holds one: a JSON object. The
 * engine owns the model and validates every key on write
 * (`platform/api/openapi.yaml` `PolicyDocument`), so the console checks
 * only that the text is an object and leaves the rest to the engine's
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

/**
 * What the engine refused about a document, as the form shows it: the
 * sentence, and where garam named one, the JSON Pointer to the place
 * (`platform/api/openapi.yaml` `Error.path`).
 */
export type DocumentProblem = { message: string; path?: string };

/** Where in a document a problem is, as the structured view places it. */
export type ProblemPlace =
  | { at: "document" }
  | { at: "version" }
  | { at: "statement"; index: number; key?: StatementKey };

/** The statement keys the structured view gives a control of its own. */
export type StatementKey = "Sid" | "Effect" | "Action" | "Resource";

const STATEMENT_KEYS: readonly StatementKey[] = [
  "Sid",
  "Effect",
  "Action",
  "Resource",
];

/**
 * Places a pointer from a refusal. A pointer below a statement's list
 * (`/Statement/0/Action/2`) places at the list; one the view has no
 * control for places at the statement, or at the document.
 */
export function placeProblem(path: string | undefined): ProblemPlace {
  const tokens = path === undefined ? null : pointerTokens(path);
  if (tokens === null || tokens.length === 0) return { at: "document" };
  if (tokens[0] === "Version") return { at: "version" };
  if (tokens[0] !== "Statement" || tokens.length < 2) return { at: "document" };
  const index = Number(tokens[1]);
  if (!Number.isInteger(index) || index < 0) return { at: "document" };
  const key = STATEMENT_KEYS.find((candidate) => candidate === tokens[2]);
  return { at: "statement", index, key };
}

/** A JSON object, read without asserting its keys. */
const JsonObject = z.record(z.string(), z.unknown());

/** A statement as the structured view edits it. */
export type StatementFields = {
  sid: string;
  /** As written: a value other than Allow or Deny is shown, not dropped. */
  effect: string;
  actions: string[];
  resources: string[];
  conditioned: boolean;
};

/**
 * The key `object` holds for the model's `name`. The engine matches a
 * key without regard to case, as encoding/json matches a struct field,
 * so `"action"` is the Action; an exact spelling wins where both are
 * written. Undefined where the object holds neither.
 */
export function keyFor(
  object: Record<string, unknown>,
  name: string,
): string | undefined {
  if (Object.hasOwn(object, name)) return name;
  const folded = name.toLowerCase();
  return Object.keys(object).find((key) => key.toLowerCase() === folded);
}

/** The value under the model's `name`, matched as `keyFor` matches. */
export function valueFor(
  object: Record<string, unknown>,
  name: string,
): unknown {
  const key = keyFor(object, name);
  return key === undefined ? undefined : object[key];
}

/**
 * `object` with `value` under the model's `name`: written over the key
 * already holding it, in whatever case, so an edit never adds a second
 * spelling beside the first.
 */
export function withValue<T extends Record<string, unknown>>(
  object: T,
  name: string,
  value: unknown,
): T {
  return { ...object, [keyFor(object, name) ?? name]: value };
}

/**
 * The strings of a list, or null for any other value. The engine takes
 * Action and Resource as lists of strings only, and refuses anything
 * else as `policy_document_wrong_type`; the view does not edit such a
 * value into a list, so it is left to the JSON view.
 */
function stringList(value: unknown): string[] | null {
  if (value === undefined) return [];
  if (!Array.isArray(value)) return null;
  return value.every((item): item is string => typeof item === "string")
    ? value
    : null;
}

/**
 * The document's statements, or null where `Statement` is neither
 * absent nor a list — a shape only the JSON view can show.
 */
export function statementsOf(document: PolicyDocument): unknown[] | null {
  const statements = valueFor(document, "Statement");
  if (statements === undefined) return [];
  return Array.isArray(statements) ? statements : null;
}

/**
 * A statement's fields, or null where it is not an object or holds an
 * Action or Resource that is not a list of strings: a shape the
 * controls cannot hold without changing it, so only JSON edits it.
 */
export function readStatement(statement: unknown): StatementFields | null {
  const parsed = JsonObject.safeParse(statement);
  if (!parsed.success) return null;
  const raw = parsed.data;
  const actions = stringList(valueFor(raw, "Action"));
  const resources = stringList(valueFor(raw, "Resource"));
  if (actions === null || resources === null) return null;
  const sid = valueFor(raw, "Sid");
  const effect = valueFor(raw, "Effect");
  return {
    sid: typeof sid === "string" ? sid : "",
    effect: typeof effect === "string" ? effect : "",
    actions,
    resources,
    conditioned: keyFor(raw, "Condition") !== undefined,
  };
}

/**
 * `statement` with the fields the view edits written over it, each
 * under the key already holding it in whatever case. Keys the view
 * does not edit, Condition among them, are kept as they were, and an
 * empty Sid that was never there is not added.
 */
export function writeStatement(
  statement: unknown,
  fields: Omit<StatementFields, "conditioned">,
): Record<string, unknown> {
  const parsed = JsonObject.safeParse(statement);
  let raw: Record<string, unknown> = parsed.success ? parsed.data : {};
  if (fields.sid !== "" || keyFor(raw, "Sid") !== undefined) {
    raw = withValue(raw, "Sid", fields.sid);
  }
  raw = withValue(raw, "Effect", fields.effect);
  raw = withValue(raw, "Action", fields.actions);
  return withValue(raw, "Resource", fields.resources);
}

/**
 * Whether any statement of `document` carries a `Condition`. garam
 * stores the key and evaluates nothing from it
 * (`docs/architecture/authz.md` §Policy document), so such a statement
 * applies as if it had none.
 */
export function hasCondition(document: PolicyDocument): boolean {
  const statements = statementsOf(document);
  return (
    statements !== null &&
    statements.some((statement) => {
      const parsed = JsonObject.safeParse(statement);
      return parsed.success && keyFor(parsed.data, "Condition") !== undefined;
    })
  );
}
