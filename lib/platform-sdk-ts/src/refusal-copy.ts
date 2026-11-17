/**
 * The sentences a person reads when garam refuses a request or cannot
 * be reached, and the one place they are written. A refusal is matched
 * by the fields `platform/api/openapi.yaml` gives every error a stable
 * meaning — the HTTP status and the `kind` beside it — never by the
 * English of `message`, which garam may reword at any release.
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
 * optional stable code and JSON Pointer `platform/api/openapi.yaml`
 * `Error` carries where the kind alone does not say which refusal it
 * was.
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
  | "grantOrgConsent"
  | "approveAgentEnrollment"
  | "withdrawAgentEnrollment"
  | "revokeAgentSupervisor"
  | "decideDeviceCode"
  | "revokeSession";

/** `status kind`, the stable pair a refusal is matched by. */
type RefusalKey = `${number} ${string}`;

const INVITATION_NOT_FOUND =
  "This invitation link isn't valid. Check that you opened the whole link, or ask for a new invitation.";

// garam answers an accepted invitation and an expired one with the same
// status and kind (`failed_precondition`), so one sentence covers both.
const INVITATION_SPENT =
  "This invitation can no longer be used: it was already accepted or it has expired. Ask an admin of the organization for a new one.";

const CONSENT_WITH_ITSELF =
  "That is this organization's own identifier. A consent is granted to another organization.";

const CONSENT_PARTNER_NOT_FOUND =
  "No organization has that identifier. Check it with the other organization and try again.";

const PREPARED_UNREADABLE =
  "That isn't what garam standalone prepare prints. Paste its whole output: requestId and secretSha256.";

const APPROVAL_REQUEST_TAKEN =
  "garam already holds this request identifier with other values. The host needs a fresh preparation.";

// garam answers an approval withdrawn or expired, and a replacement
// whose assignment moved, with the same status and kind.
const APPROVAL_NO_LONGER_APPLIES =
  "garam refused it: this request's approval was withdrawn or expired, or, for a replacement, the agent has moved since this page read it. Reload to see where the agent stands; a withdrawn or expired request needs a fresh preparation.";

const APPROVAL_SPENT =
  "A host has already enrolled with this approval. End what it registered by revoking its supervisor.";

const SUPERVISOR_ENDED =
  "This supervisor is no longer current: it was revoked or replaced. Reload the page to see where it stands.";

const BY_OPERATION: Record<
  RefusalOperation,
  Partial<Record<RefusalKey, string>>
> = {
  decideDeviceCode: {
    "404 not_found":
      "That code isn't waiting for approval. Check it against your terminal, or run garam login again: a code lasts ten minutes.",
  },
  revokeSession: {
    "404 not_found": "That session has already ended.",
  },
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
  // garam answers a grant naming this organization itself with 400 and
  // one naming no organization it holds with 404, neither for a reason
  // the generic sentences say.
  grantOrgConsent: {
    "400 invalid_argument": CONSENT_WITH_ITSELF,
    "404 not_found": CONSENT_PARTNER_NOT_FOUND,
  },
  approveAgentEnrollment: {
    "409 already_exists": APPROVAL_REQUEST_TAKEN,
    "409 failed_precondition": APPROVAL_NO_LONGER_APPLIES,
  },
  withdrawAgentEnrollment: { "409 already_exists": APPROVAL_SPENT },
  revokeAgentSupervisor: { "404 not_found": SUPERVISOR_ENDED },
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
  consentWithItself: CONSENT_WITH_ITSELF,
  consentPartnerNotFound: CONSENT_PARTNER_NOT_FOUND,
  preparedUnreadable: PREPARED_UNREADABLE,
  approvalRequestTaken: APPROVAL_REQUEST_TAKEN,
  approvalNoLongerApplies: APPROVAL_NO_LONGER_APPLIES,
  approvalSpent: APPROVAL_SPENT,
  supervisorEnded: SUPERVISOR_ENDED,
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
 * The sentence each documented reason reads as. A reason garam adds
 * later and this file does not know is read by its status and kind
 * instead.
 */
const BY_REASON: Record<string, () => string> = {
  change_not_applied: () =>
    "Your change is saved but not yet in effect. Wait a moment and check again; don't repeat the request.",
  change_refused: () =>
    "Your change is saved but the policy service refused it, so it will not take effect on its own. Don't repeat the request; ask whoever runs garam to look at it.",
  invitation_accepted: () =>
    "This invitation has already been used. Ask an admin of the organization for a new one.",
  invitation_expired: () =>
    "This invitation has expired. Ask an admin of the organization to send you a new one.",
  key_issuance_unconfigured: () =>
    "This garam doesn't issue operator keys: its machine token URL isn't set. Whoever runs garam sets it.",
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
  if (byReason !== undefined) return byReason();
  const key: RefusalKey = `${answer.status} ${answer.kind}`;
  const mapped =
    (operation === undefined ? undefined : BY_OPERATION[operation][key]) ??
    GENERIC[key];
  if (mapped !== undefined) return mapped;
  return unmappedRefusalCopy(answer.message);
}
