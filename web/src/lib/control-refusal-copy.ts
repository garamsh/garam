/**
 * The sentences a person reads when a hosted operator's control service
 * refuses a call or cannot be reached, and the one place they are
 * written. A refusal is matched by its HTTP status and the `kind` the
 * control service names, never by the English of its `message`, which
 * is written for the API's caller and never reaches a person.
 *
 * A known kind reads its sentence. A refusal naming no kind reads the
 * sentence for its status. Where the same kind or status means
 * something particular to an operation, that operation supplies the
 * sentence. Anything else reads one fixed sentence that says what to
 * try.
 */

import type { ControlOperation, ControlRefusal } from "./operation-authority";

/**
 * The refusal kinds the control service documents on the routes the
 * console calls: garam-agent-operator `docs/architecture/control.md`,
 * the table under "Each refusal has one status", at `8e1116933408`,
 * and `garam_contract_unsupported`, which `4a22cd3` (#298) added there.
 * The lifecycle, recovery and cutover kinds in that table are left
 * out: they answer routes the console does not call.
 */
export const CONTROL_REFUSAL_KINDS = [
  "invalid_api_key_ref",
  "embedding_required",
  "embedding_immutable",
  "garam_contract_unsupported",
] as const;
export type ControlRefusalKind = (typeof CONTROL_REFUSAL_KINDS)[number];

/** What a person reads where the control service did not answer. */
export const CONTROL_UNAVAILABLE =
  "The control service is unavailable, so its controls are disabled. Teams, people and agents are unaffected.";

/** What a person reads for a refusal no entry maps. */
export const CONTROL_REFUSED =
  "The control service refused this request. Try again; if it keeps refusing, ask an administrator to check the operator on its page.";

/** What a person reads for an answer outside the agreed wire. */
export const CONTROL_UNREADABLE =
  "The control service answered outside the agreed contract.";

const BY_KIND: Record<ControlRefusalKind, string> = {
  invalid_api_key_ref:
    "An API key reference is written as secret-name/key: the Secret that holds the key, a slash, and the key in it. Correct the reference and try again.",
  embedding_required:
    "This model needs an embeddings endpoint. Give the embedding's base URL and model name, then try again.",
  embedding_immutable:
    "An agent's embeddings endpoint can't be changed or removed once it is set. Keep the base URL and model name it has.",
  // Answered with 503, so the seam reads it as unavailable before its
  // kind is looked at; the sentence is the same either way.
  garam_contract_unsupported: CONTROL_UNAVAILABLE,
};

/** A refusal with no kind, keyed by its HTTP status. */
type StatusKey = `${number}`;

const BY_STATUS: Partial<Record<StatusKey, string>> = {
  "400":
    "The control service couldn't read the request the console sent. Reload the page and try again.",
  "401":
    "The control service didn't accept garam's authority for this request. Try again in a moment.",
  "403":
    "The control service refused this for you: garam no longer authorizes it, or the operator's delegation no longer covers it. Reload the page to see what you can do now.",
  "404":
    "The control service holds nothing here for this organization. Reload the page to see what it holds now.",
  "409":
    "The control service already holds a different request like this one. Reload the page and try again.",
};

const BY_OPERATION: Partial<
  Record<
    ControlOperation,
    Partial<Record<ControlRefusalKind | StatusKey, string>>
  >
> = {
  // A create copies a published version, so what is wrong with its
  // model is fixed by publishing another version, not here.
  "agent:create": {
    embedding_required:
      "This template version names a model without an embeddings endpoint, so no agent can start from it. Publish a version that names one, then create the agent from that version.",
    invalid_api_key_ref:
      "This template version's API key reference isn't written as secret-name/key, so no agent can start from it. Publish a version with a corrected reference, then create the agent from that version.",
    "403":
      "garam refused to create an agent on that controller for you. Choose another controller, or ask an admin of the organization to check the operator's delegation.",
    "404":
      "That template version or its profile is no longer published. Reload the page and choose from what is.",
    "409":
      "The control service holds an agent or request that conflicts with this one. Reload the page; an agent that was created is listed there.",
  },
  "agent-template:publish": {
    "400":
      "The control service couldn't read this version. Check every field, then publish again.",
    "404":
      "That profile version isn't published. Check the profile's name and version against the published profiles.",
    "409":
      "This conflicts with a publication the control service already recorded. Publish again.",
  },
  "agent-template:read": {
    "404":
      "That template version isn't published here any more. Reload the page to see what is.",
  },
  "agent:execution-read": {
    "404":
      "The control service holds no revision for this agent, so it has no execution to show. Reload the page to check again.",
  },
};

/** The kinds this file writes a sentence for. */
export const MAPPED_CONTROL_KINDS: readonly string[] = Object.keys(BY_KIND);

function isKnownKind(kind: string): kind is ControlRefusalKind {
  // Own keys only: a kind is server text, and `constructor` is a key
  // every object answers.
  return Object.hasOwn(BY_KIND, kind);
}

/** The sentence for a control call that did not succeed. */
export function controlFailureCopy(
  failure:
    { status: "unavailable" } | { status: "refused"; refusal: ControlRefusal },
  operation: ControlOperation,
): string {
  if (failure.status === "unavailable") return CONTROL_UNAVAILABLE;
  const { refusal } = failure;
  switch (refusal.source) {
    case "mint":
      return refusal.sentence;
    case "unreadable":
      return CONTROL_UNREADABLE;
    case "control": {
      const byOperation = BY_OPERATION[operation] ?? {};
      if (refusal.kind !== null) {
        if (!isKnownKind(refusal.kind)) return CONTROL_REFUSED;
        return byOperation[refusal.kind] ?? BY_KIND[refusal.kind];
      }
      const key: StatusKey = `${refusal.status}`;
      return byOperation[key] ?? BY_STATUS[key] ?? CONTROL_REFUSED;
    }
  }
}
