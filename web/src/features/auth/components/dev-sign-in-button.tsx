import { Button } from "@/components/ui/button";

/** The link's label and accessible name. */
export const DEV_SIGN_IN_LABEL = "Dev sign-in";

/**
 * One-click local sign-in through the backend's dev provider
 * (ADR-0010). Render it only when the backend advertises `dev` from
 * `/auth/providers`. A plain link, not a fetch — the backend answers
 * with a 302 that drives the flow and sets the session cookie on this
 * origin.
 */
export function DevSignInButton() {
  return (
    <Button asChild variant="outline">
      <a href="/api/auth/login/dev" aria-label={DEV_SIGN_IN_LABEL}>
        {DEV_SIGN_IN_LABEL}
      </a>
    </Button>
  );
}
