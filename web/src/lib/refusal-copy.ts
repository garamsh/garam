/**
 * The sentences a person reads when garam refuses a request or cannot
 * be reached, and the one place they are written. A refusal is matched
 * by the fields `api/openapi.yaml` gives every error a stable meaning —
 * the HTTP status and the `kind` beside it — never by the English of
 * `message`, which garam may reword at any release.
 *
 * Where the same status and kind mean different things to different
 * operations, the operation that asked supplies the sentence. What no
 * entry covers falls through to a generic sentence that quotes the
 * server's text, so an unmapped refusal still says something true.
 */

/** An error answer as the seam received it. */
export type RefusalAnswer = { status: number; kind: string; message: string };

/**
 * The operations whose refusals read differently from the generic ones.
 * An operation not listed here takes the generic sentences only.
 */
export type RefusalOperation =
  "acceptInvitation" | "registerPassword" | "createPolicy" | "addPolicyVersion";

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

/** The sentence for a refusal garam answered with. */
export function refusalCopy(
  answer: RefusalAnswer,
  operation?: RefusalOperation,
): string {
  const key: RefusalKey = `${answer.status} ${answer.kind}`;
  const mapped =
    (operation === undefined ? undefined : BY_OPERATION[operation][key]) ??
    GENERIC[key];
  if (mapped !== undefined) return mapped;
  return unmappedRefusalCopy(answer.message);
}
