/**
 * Where a visitor sent to sign in returns to afterwards. The proxy
 * carries the console address they asked for to `/login` as `next`,
 * and the sign-in and register forms hand it back once a session
 * exists — which is how an invitation link opened signed out keeps its
 * token through either.
 *
 * `next` arrives in a URL anyone can write, so only a path on the app
 * the sign-in is for is returned to (ADR-0135): inside `/console` for
 * the console, any path of cowork's for cowork. Anything else — another
 * origin, a protocol-relative `//host`, a backslash a browser reads as
 * a slash — is dropped, and the caller lands where it would have
 * without one.
 */

import type { HandoffApp } from "./handoff";

/** The parameter `/login` and `/register` read the return path from. */
export const RETURN_PARAM = "next";

/**
 * The request header an app's `proxy.ts` stamps a request with: the path
 * and query it asked for, so a page or an action that finds the
 * session gone can send the visitor to sign in carrying it. The proxy
 * overwrites whatever a client sent under this name, and every reader
 * passes it through `safeReturnPath` all the same.
 */
export const RETURN_PATH_HEADER = "x-garam-return-path";

const INVITATION_PATH = /^\/console\/invitations\/([^/?#]+)$/;

/** The bound on a return path, in UTF-8 bytes, as the backend counts. */
const MAX_RETURN_PATH_BYTES = 2048;

/** Where a sign-in for each app lands when it carries no return path. */
export const DEFAULT_RETURN_PATH: Record<HandoffApp, string> = {
  console: "/console",
  cowork: "/",
};

/**
 * `value` as a path on `app` to return to, or null where it is not one:
 * an absolute path with no host, inside `/console` for the console,
 * well-formed and at most `MAX_RETURN_PATH_BYTES` in UTF-8, with no
 * backslash, control character or dot segment.
 * `platform/internal/api/return_path.go` holds the same rule for the
 * provider sign-in, and
 * `platform/internal/api/testdata/return-path-vectors.json` is the
 * table both are tested against.
 */
export function safeReturnPath(
  value: unknown,
  app: HandoffApp = "console",
): string | null {
  if (typeof value !== "string" || !value.isWellFormed()) return null;
  if (new TextEncoder().encode(value).length > MAX_RETURN_PATH_BYTES) {
    return null;
  }
  if (!value.startsWith("/") || value.startsWith("//")) return null;
  if (
    app === "console" &&
    value !== "/console" &&
    !value.startsWith("/console/")
  ) {
    return null;
  }
  // A backslash or a control character is read by some browsers as a
  // path separator or dropped, either of which can turn a console path
  // into another origin's.
  if (/[\\\u0000-\u001f\u007f]/.test(value)) return null;
  if (hasDotSegment(value)) return null;
  return value;
}

/**
 * Whether `path` holds a `.` or `..` segment before its query, written
 * plainly or percent-encoded, which a browser's URL parser resolves. A
 * fragment counts as path, as `platform/internal/api/return_path.go`
 * states.
 */
function hasDotSegment(path: string): boolean {
  return path
    .split("?", 1)[0]
    .split("/")
    .some((segment) => {
      const plain = segment.toLowerCase().replaceAll("%2e", ".");
      return plain === "." || plain === "..";
    });
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
