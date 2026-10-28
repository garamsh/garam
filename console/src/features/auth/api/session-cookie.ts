import "server-only";

import { cookies } from "next/headers";

import { SESSION_COOKIE } from "@garam/platform-client/session";

/**
 * Plants on this origin the session cookie the backend issued.
 *
 * What reaches the browser is the Server Action's own response, so the
 * backend's `Set-Cookie` arrives at the Next.js server and stops
 * there. A sign-in that did not replant it would leave the browser
 * signed out holding a session that exists.
 *
 * The attributes are read from the header rather than restated here:
 * `Secure` and `Max-Age` are the backend's configuration, and a
 * console deciding either for itself would be a second policy to keep
 * in step with that one.
 */
export async function keepSessionCookie(response: Response): Promise<void> {
  const header = response.headers
    .getSetCookie()
    .find((candidate) => candidate.startsWith(`${SESSION_COOKIE}=`));
  if (header === undefined) {
    // A sign-in route that answered 2xx without the cookie it exists to
    // set is a bug rather than a refusal: nothing a person types
    // reaches it, so it belongs at the boundary and not in the form.
    throw new Error("garam signed in without issuing a session cookie");
  }
  const [pair, ...rest] = header.split(";");
  const attributes = new Map(
    rest.map((attribute) => {
      const [name, ...value] = attribute.trim().split("=");
      return [name.toLowerCase(), value.join("=")];
    }),
  );
  const maxAge = Number(attributes.get("max-age"));
  (await cookies()).set({
    name: SESSION_COOKIE,
    value: pair.slice(SESSION_COOKIE.length + 1),
    path: attributes.get("path") ?? "/",
    httpOnly: attributes.has("httponly"),
    secure: attributes.has("secure"),
    sameSite: sameSiteOf(attributes.get("samesite")),
    ...(Number.isInteger(maxAge) && maxAge > 0 ? { maxAge } : {}),
  });
}

/** The `SameSite` the header carries; Lax where it names none. */
function sameSiteOf(value: string | undefined): "lax" | "strict" | "none" {
  switch (value?.toLowerCase()) {
    case "strict":
      return "strict";
    case "none":
      return "none";
    default:
      return "lax";
  }
}
