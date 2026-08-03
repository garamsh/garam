import Image from "next/image";

/** Approved label, carried by the button image and its accessible name. */
export const GOOGLE_SIGN_IN_LABEL = "Sign in with Google";

/**
 * Google's own sign-in button asset, unmodified
 * (developers.google.com/identity/branding-guidelines): the mark,
 * colors, font, and spacing are baked into the image, and the light and
 * dark variants swap with the theme. A plain link, not a fetch — the
 * backend answers with a 302 to Google and sets the PKCE cookies on
 * this origin.
 */
export function GoogleSignInButton() {
  return (
    <a
      href="/api/auth/login/google"
      aria-label={GOOGLE_SIGN_IN_LABEL}
      className="inline-block rounded-full focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
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
    </a>
  );
}
