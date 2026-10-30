import Image from "next/image";

import { APP_PARAM, type HandoffApp } from "@garam/platform-sdk-ts/handoff";
import { RETURN_PARAM } from "@garam/platform-sdk-ts/return-path";

/** Approved label, carried by the button image and its accessible name. */
export const GOOGLE_SIGN_IN_LABEL = "Sign in with Google";

/**
 * Google's own sign-in button asset, unmodified
 * (developers.google.com/identity/branding-guidelines): the mark,
 * colors, font, and spacing are baked into the image, and the light and
 * dark variants swap with the theme.
 *
 * A form posted straight to the backend rather than a fetch: it answers
 * with a 302 to Google and sets the PKCE cookies on this origin.
 * `returnPath` — where the visitor was going, an invitation among them
 * — and the `app` it is a path on travel in the body, so they reach no
 * URL; the backend holds them with the login's state and lands there
 * once signed in.
 */
export function GoogleSignInButton({
  app = "console",
  returnPath = null,
}: {
  app?: HandoffApp;
  returnPath?: string | null;
}) {
  return (
    <form method="post" action="/api/auth/login/google">
      <input type="hidden" name={APP_PARAM} value={app} />
      {returnPath !== null && (
        <input type="hidden" name={RETURN_PARAM} value={returnPath} />
      )}
      <button
        type="submit"
        aria-label={GOOGLE_SIGN_IN_LABEL}
        className="inline-block cursor-pointer rounded-full focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
      >
        <Image
          src="/google/sign-in-with-google-light.png"
          alt=""
          width={180}
          height={40}
          priority
          className="dark:hidden"
        />
        <Image
          src="/google/sign-in-with-google-dark.png"
          alt=""
          width={180}
          height={40}
          priority
          className="hidden dark:block"
        />
      </button>
    </form>
  );
}
