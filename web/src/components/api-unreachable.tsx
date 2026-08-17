/**
 * The notice a page shows in place of data whose read never reached the
 * API. Every page states it the same way, because a reader has to be
 * able to tell it from the two states it rules out — signed out, and
 * refused.
 *
 * A page renders it rather than throwing: a production build replaces
 * the message of an error thrown out of a Server Component before
 * `error.tsx` sees it, so a boundary cannot name the API.
 */

/**
 * The notice's heading. Each page gives it the rank its own outline
 * leaves free.
 */
export const API_UNREACHABLE_TITLE = "Cannot reach the API";

/**
 * The notice's paragraph, for the caller's own `<p>`. `subject` names
 * what this page cannot show; nothing else about it varies by page.
 */
export function ApiUnreachableReason({ subject }: { subject: string }) {
  return (
    <>
      The garam API is not answering, so this page cannot show {subject}. This
      is not a sign-in problem and not a permission problem: try again once the
      API is back.
    </>
  );
}
