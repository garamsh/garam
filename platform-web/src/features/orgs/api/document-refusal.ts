import "server-only";

import { GaramApiError, refusal } from "@/lib/garam";
import type { RefusalOperation } from "@garam/platform-sdk-ts/refusal-copy";

/**
 * A refusal a policy-document form shows. `field` says the engine
 * refused the document itself, so the form attaches the sentence to
 * the editor rather than to the action; `path` is where garam said the
 * fault is, when it said.
 */
export type DocumentRefusal = {
  status: "error";
  message: string;
  field?: "document";
  path?: string;
};

/**
 * The refusal of a write carrying a policy document. A 400
 * `invalid_argument` is the engine refusing the document — the one
 * refusal those operations answer with that pair — and keeps the place
 * garam named; every other refusal is about the action, not the field.
 */
export function documentRefusal(
  error: unknown,
  operation: RefusalOperation,
): DocumentRefusal {
  const refused = refusal(error, operation);
  if (
    !(error instanceof GaramApiError) ||
    error.status !== 400 ||
    error.kind !== "invalid_argument"
  ) {
    return refused;
  }
  // The place garam named, where it named one
  // (`platform/api/openapi.yaml` `Error.path`).
  const path = error.reason?.path;
  return path === undefined
    ? { ...refused, field: "document" }
    : { ...refused, field: "document", path };
}
