/**
 * The sentences a person reads when garam refuses a request or cannot
 * be reached, and the one place they are written. A refusal is matched
 * by the fields `api/openapi.yaml` gives every error a stable meaning —
 * the HTTP status and the `kind` beside it — never by the English of
 * `message`, which garam may reword at any release.
 *
 * A refusal carrying a `reason` garam documents reads that reason's
 * sentence first: it names the rule that refused, which the status and
 * kind cannot. Where the same status and kind mean different things to
 * different operations, the operation that asked supplies the sentence.
 * What no entry covers falls through to a generic sentence that quotes
 * the server's text, so an unmapped refusal still says something true.
 */

/**
 * An error answer as the seam received it. `reason` and `path` are the
 * optional stable code and JSON Pointer `api/openapi.yaml` `Error`
 * carries where the kind alone does not say which refusal it was.
 */
export type RefusalAnswer = {
  status: number;
  kind: string;
  message: string;
  reason?: string;
  path?: string;
};

/**
 * The operations whose refusals read differently from the generic ones.
 * An operation not listed here takes the generic sentences only.
 */
export type RefusalOperation =
  | "acceptInvitation"
  | "registerPassword"
  | "createPolicy"
  | "addPolicyVersion"
  | "grantOrgConsent"
  | "mintOperatorEnrollment";

/** `status kind`, the stable pair a refusal is matched by. */
type RefusalKey = `${number} ${string}`;

const INVITATION_NOT_FOUND =
  "This invitation link isn't valid. Check that you opened the whole link, or ask for a new invitation.";

// garam answers an accepted invitation and an expired one with the same
// status and kind (`failed_precondition`), so one sentence covers both.
const INVITATION_SPENT =
  "This invitation can no longer be used: it was already accepted or it has expired. Ask an admin of the organization for a new one.";

// garam answers every rule a policy document breaks with the same status
// and kind (`invalid_argument`), so the sentence names the rules rather
// than the one that failed.
const POLICY_DOCUMENT_REFUSED =
  "This policy document can't be saved. It needs a Version and at least one statement, and every statement needs an Effect of Allow or Deny, an Action and a Resource. Keys other than Version, Statement, Sid, Effect, Action, Resource and Condition aren't allowed.";

const CONSENT_WITH_ITSELF =
  "That is this organization's own identifier. A consent is granted to another organization.";

const CONSENT_PARTNER_NOT_FOUND =
  "No organization has that identifier. Check it with the other organization and try again.";

// garam refuses a token for an operator it has issued a certificate,
// whether or not that certificate has since run out (ADR-0067).
const OPERATOR_ALREADY_ENROLLED =
  "This operator has enrolled, so garam won't issue it another token: that would be a second way into an identity that already has one. An operator whose certificate was lost or ran out is recovered with garam's out-of-band certificate command.";

const OPERATOR_IDENTIFIER_INVALID =
  "That isn't an operator identifier garam accepts. Reload the page and try again from the operator's own page.";

const BY_OPERATION: Record<
  RefusalOperation,
  Partial<Record<RefusalKey, string>>
> = {
  acceptInvitation: {
    "404 not_found": INVITATION_NOT_FOUND,
    "409 failed_precondition": INVITATION_SPENT,
    "409 already_exists": "You're already a member of this organization.",
  },
  registerPassword: {
    "404 not_found": INVITATION_NOT_FOUND,
    "409 failed_precondition": INVITATION_SPENT,
    "409 already_exists": "That username is taken. Choose another one.",
  },
  createPolicy: { "400 invalid_argument": POLICY_DOCUMENT_REFUSED },
  addPolicyVersion: { "400 invalid_argument": POLICY_DOCUMENT_REFUSED },
  // garam answers a grant naming this organization itself with 400 and
  // one naming no organization it holds with 404, neither for a reason
  // the generic sentences say.
  grantOrgConsent: {
    "400 invalid_argument": CONSENT_WITH_ITSELF,
    "404 not_found": CONSENT_PARTNER_NOT_FOUND,
  },
  mintOperatorEnrollment: {
    "400 invalid_argument": OPERATOR_IDENTIFIER_INVALID,
    "409 failed_precondition": OPERATOR_ALREADY_ENROLLED,
  },
};

/**
 * The sentences every operation shares. Exported so a form's tests can
 * name which one it rendered; the words themselves are this file's.
 */
export const REFUSAL_COPY = {
  unauthenticated: "Your session has ended. Sign in again to continue.",
  permissionDenied:
    "You don't have permission to do this. An owner or admin of the organization can grant it.",
  notFound:
    "This no longer exists. It may have been deleted — reload the page to see what's there now.",
  alreadyExists: "That already exists, so nothing was changed.",
  tooManyRequests:
    "garam is receiving too many requests. Wait a moment and try again.",
  unreachable: "garam can't be reached right now. Try again in a moment.",
  invitationNotFound: INVITATION_NOT_FOUND,
  invitationSpent: INVITATION_SPENT,
  policyDocumentRefused: POLICY_DOCUMENT_REFUSED,
  consentWithItself: CONSENT_WITH_ITSELF,
  consentPartnerNotFound: CONSENT_PARTNER_NOT_FOUND,
  operatorAlreadyEnrolled: OPERATOR_ALREADY_ENROLLED,
  operatorIdentifierInvalid: OPERATOR_IDENTIFIER_INVALID,
} as const;

const GENERIC: Partial<Record<RefusalKey, string>> = {
  "401 unauthenticated": REFUSAL_COPY.unauthenticated,
  "403 permission_denied": REFUSAL_COPY.permissionDenied,
  "404 not_found": REFUSAL_COPY.notFound,
  "409 already_exists": REFUSAL_COPY.alreadyExists,
  "429 resource_exhausted": REFUSAL_COPY.tooManyRequests,
};

/** What a person reads when garam could not be reached at all. */
export const UNREACHABLE_COPY = REFUSAL_COPY.unreachable;

/** The generic sentence for a refusal no entry maps, quoting garam. */
export function unmappedRefusalCopy(message: string): string {
  return `garam couldn't complete this request. It answered: “${message}”`;
}

/**
 * A JSON Pointer into a policy document as a sentence names the place:
 * `/Statement/1/Effect` is "statement 2's Effect".
 */
function placeIn(path: string | undefined): string {
  const tokens = (path ?? "")
    .split("/")
    .slice(1)
    .map((token) =>
      canonical(token.replaceAll("~1", "/").replaceAll("~0", "~")),
    );
  if (tokens.length === 0) return "the document";
  if (tokens[0] !== "Statement" || tokens.length === 1)
    return tokens.join(" › ");
  const statement = `statement ${Number(tokens[1]) + 1}`;
  if (tokens.length === 2) return statement;
  const item = tokens.length > 3 ? `, item ${Number(tokens[3]) + 1}` : "";
  return `${statement}'s ${tokens[2]}${item}`;
}

/** The keys the policy format names, as it spells them. */
const POLICY_KEYS = [
  "Version",
  "Statement",
  "Sid",
  "Effect",
  "Action",
  "Resource",
  "Condition",
];

/**
 * A pointer token in the format's own spelling where it names a key the
 * format holds. garam already points at known keys by that spelling
 * (`api/openapi.yaml` `Error.path`); this keeps a sentence in it
 * whatever case reaches here. Any other token is shown as written.
 */
function canonical(token: string): string {
  return (
    POLICY_KEYS.find((key) => key.toLowerCase() === token.toLowerCase()) ??
    token
  );
}

/** The pointer to what holds the place `path` leads to. */
function parentOf(path: string | undefined): string {
  return (path ?? "").split("/").slice(0, -1).join("/");
}

/** Capitalizes the first letter, for a place that opens a sentence. */
function opening(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1);
}

/**
 * The sentence each documented reason reads as, given the pointer it
 * came with. A reason garam adds later and this file does not know is
 * read by its status and kind instead.
 */
const BY_REASON: Record<string, (path: string | undefined) => string> = {
  invitation_accepted: () =>
    "This invitation has already been used. Ask an admin of the organization for a new one.",
  invitation_expired: () =>
    "This invitation has expired. Ask an admin of the organization to send you a new one.",
  key_issuance_unconfigured: () =>
    "This garam doesn't issue operator keys: its machine token URL isn't set. Whoever runs garam sets it.",
  policy_document_malformed: () =>
    "This policy document isn't valid JSON. Check for a missing quote, comma or bracket.",
  policy_document_trailing_data: () =>
    "There is text after the end of the policy document. Remove it and save again.",
  policy_document_wrong_type: (path) =>
    path === undefined || path === ""
      ? "A policy document must be a JSON object."
      : `${opening(placeIn(path))} has the wrong type of value.`,
  policy_document_unknown_key: (path) =>
    `${opening(placeIn(path))} isn't part of the policy format. The keys it allows are Version, Statement, Sid, Effect, Action, Resource and Condition.`,
  policy_document_version_missing: () => "The document needs a Version.",
  policy_document_statement_empty: () =>
    "The document needs at least one statement.",
  policy_document_effect_invalid: (path) =>
    `${opening(placeIn(path))} must be Allow or Deny.`,
  policy_document_action_missing: (path) =>
    `${opening(placeIn(parentOf(path)))} needs at least one Action.`,
  policy_document_resource_missing: (path) =>
    `${opening(placeIn(parentOf(path)))} needs at least one Resource.`,
};

/** The reasons this file writes a sentence for. */
export const MAPPED_REASONS: readonly string[] = Object.keys(BY_REASON);

/**
 * Documented reasons this file reads by status and kind on purpose.
 * None today; a code goes here, with why, rather than being missed.
 */
export const UNMAPPED_REASONS: readonly string[] = [];

/** The sentence for a refusal garam answered with. */
export function refusalCopy(
  answer: RefusalAnswer,
  operation?: RefusalOperation,
): string {
  // Own keys only: a reason is server text, and `constructor` is a key
  // every object answers.
  const byReason =
    answer.reason !== undefined && Object.hasOwn(BY_REASON, answer.reason)
      ? BY_REASON[answer.reason]
      : undefined;
  if (byReason !== undefined) return byReason(answer.path);
  const key: RefusalKey = `${answer.status} ${answer.kind}`;
  const mapped =
    (operation === undefined ? undefined : BY_OPERATION[operation][key]) ??
    GENERIC[key];
  if (mapped !== undefined) return mapped;
  return unmappedRefusalCopy(answer.message);
}
