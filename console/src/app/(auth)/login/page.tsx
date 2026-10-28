import type { Metadata } from "next";
import Link from "next/link";
import { redirect } from "next/navigation";

import {
  Card,
  CardContent,
  CardFooter,
  CardHeader,
} from "@garam/design/ui/card";
import {
  GoogleSignInButton,
  PasswordSignInForm,
  getSession,
  listProviders,
} from "@/features/auth";
import {
  RETURN_PARAM,
  safeReturnPath,
  withReturnPath,
} from "@garam/platform-client/return-path";

import { ApiUnreachableCard } from "../_components/api-unreachable-card";
import {
  InvitationContext,
  readInvitationContext,
} from "../_components/invitation-context";

/**
 * Head metadata for the sign-in route; Next.js reads it by name. The
 * return path can hold an invitation token, so no link followed from
 * here sends it on as a referrer.
 */
export const metadata: Metadata = {
  title: "Sign in — garam",
  referrer: "no-referrer",
};

type Props = {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
};

/**
 * The way in. A visitor who already holds a valid session is sent on
 * to the console instead of being offered the form again. An API that
 * did not answer is said out loud: every way in from here runs through
 * it, so offering the buttons would be offering a door that is shut.
 *
 * Which provider buttons appear is the backend's answer rather than
 * this build's assumption (ADR-0010): a provider it has not registered
 * has no login route, so a button for one would lead to a 404.
 *
 * A visitor the proxy sent here carries where they were going as the
 * return path, and signing in lands there. Where that is an invitation,
 * the page names the organization and role it admits to, and the way to
 * register carries the invitation along.
 */
export default async function LoginPage({ searchParams }: Props) {
  const returnPath = safeReturnPath((await searchParams)[RETURN_PARAM]);
  const session = await getSession();
  if (session.status === "signed-in") {
    redirect(returnPath ?? "/console");
  }
  if (session.status === "unreachable") {
    return <ApiUnreachableCard subject="the way in" />;
  }
  const [providers, invitation] = await Promise.all([
    listProviders(),
    readInvitationContext(returnPath),
  ]);
  const offersGoogle = providers.includes("google");
  return (
    <Card className="w-full max-w-sm [--card-spacing:--spacing(6)]">
      <CardHeader className="gap-2">
        <h1 className="text-xl tracking-tight">
          {invitation?.usable
            ? `Sign in to join ${invitation.orgName}`
            : "Sign in to garam"}
        </h1>
        <p className="text-pretty text-muted-foreground">
          {/* Only where the backend registered a provider can a
              sign-in create an account, and a failed read is not
              told apart from none. The membership sentence below
              holds in every configuration. */}
          {invitation?.usable
            ? "Sign in to accept the invitation, or create an account if you don't have one yet."
            : `${
                offersGoogle
                  ? "Use the account you registered, or the Google account your organization invited. Signing in with Google creates an account the first time."
                  : "Use the account you registered."
              } Membership in an organization is by invitation, not by signup.`}
        </p>
      </CardHeader>
      <CardContent className="flex flex-col gap-6">
        {invitation !== null && <InvitationContext context={invitation} />}
        <PasswordSignInForm returnPath={returnPath} />
        {offersGoogle && (
          <>
            <div className="flex items-center gap-3">
              <span aria-hidden className="h-px flex-1 bg-border" />
              <span className="text-muted-foreground">or</span>
              <span aria-hidden className="h-px flex-1 bg-border" />
            </div>
            <div className="flex flex-col items-center gap-3">
              <GoogleSignInButton returnPath={returnPath} />
            </div>
          </>
        )}
      </CardContent>
      <CardFooter className="flex-col gap-2 text-muted-foreground">
        <p>
          {invitation?.usable
            ? "No garam account yet?"
            : "Holding an invitation and no account yet?"}{" "}
          <Link
            href={withReturnPath("/register", returnPath)}
            // The return path can hold an invitation token: a prefetch
            // would send it in a request nobody asked for.
            prefetch={false}
            className="text-foreground underline underline-offset-4"
          >
            Create one
          </Link>
        </p>
        <Link href="/" className="transition-colors hover:text-foreground">
          Back to the home page
        </Link>
      </CardFooter>
    </Card>
  );
}
