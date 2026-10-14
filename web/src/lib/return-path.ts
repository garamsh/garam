/**
 * Where a visitor sent to sign in returns to afterwards. The proxy
 * carries the console address they asked for to `/login` as `next`,
 * and the sign-in and register forms hand it back once a session
 * exists — which is how an invitation link opened signed out keeps its
 * token through either.
 *
 * `next` arrives in a URL anyone can write, so only a path inside the
 * console is returned to. Anything else — another origin, a
 * protocol-relative `//host`, a backslash a browser reads as a slash —
 * is dropped, and the caller lands where it would have without one.
 */

/** The parameter `/login` and `/register` read the return path from. */
export const RETURN_PARAM = "next";

const INVITATION_PATH = /^\/console\/invitations\/([^/?#]+)$/;

/** `value` as a console path to return to, or null where it is not one. */
export function safeReturnPath(value: unknown): string | null {
  if (typeof value !== "string" || value.length > 2048) return null;
  if (value !== "/console" && !value.startsWith("/console/")) return null;
  // A backslash or a control character is read by some browsers as a
  // path separator or dropped, either of which can turn a console path
  // into another origin's.
  if (/[\\\u0000-\u001f\u007f]/.test(value)) return null;
  return value;
}

/**
 * The invitation token a return path carries, or null where the path is
 * not an invitation's: `/console/invitations/{token}` and nothing below
 * or beside it.
 */
export function invitationTokenOf(path: string | null): string | null {
  const match = path === null ? null : INVITATION_PATH.exec(path);
  if (match === null) return null;
  try {
    return decodeURIComponent(match[1]);
  } catch {
    return null;
  }
}

/** `base` with `path` attached as its return parameter, where there is one. */
export function withReturnPath(base: string, path: string | null): string {
  return path === null
    ? base
    : `${base}?${new URLSearchParams({ [RETURN_PARAM]: path })}`;
}
