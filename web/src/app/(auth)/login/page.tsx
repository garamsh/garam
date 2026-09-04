import type { Metadata } from "next";
import Link from "next/link";
import { redirect } from "next/navigation";

import {
  Card,
  CardContent,
  CardFooter,
  CardHeader,
} from "@/components/ui/card";
import {
  GoogleSignInButton,
  PasswordSignInForm,
  getSession,
  listProviders,
} from "@/features/auth";

import { ApiUnreachableCard } from "../_components/api-unreachable-card";

/** Head metadata for the sign-in route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Sign in — garam" };

/**
 * The way in. A visitor who already holds a valid session is sent on
 * to the console instead of being offered the form again. An API that
 * did not answer is said out loud: every way in from here runs through
 * it, so offering the buttons would be offering a door that is shut.
 *
 * Which provider buttons appear is the backend's answer rather than
 * this build's assumption (ADR-0010): a provider it has not registered
 * has no login route, so a button for one would lead to a 404.
 */
export default async function LoginPage() {
  const session = await getSession();
  if (session.status === "signed-in") {
    redirect("/console");
  }
  if (session.status === "unreachable") {
    return <ApiUnreachableCard subject="the way in" />;
  }
  const offersGoogle = (await listProviders()).includes("google");
  return (
    <Card className="w-full max-w-sm [--card-spacing:--spacing(6)]">
      <CardHeader className="gap-2">
        <h1 className="font-heading text-2xl font-semibold tracking-tight">
          Sign in to garam
        </h1>
        <p className="text-sm text-pretty text-muted-foreground">
          {/* Only where the backend registered a provider can a
              sign-in create an account, and a failed read is not
              told apart from none. The membership sentence below
              holds in every configuration. */}
          {offersGoogle
            ? "Use the account you registered, or the Google account your organization invited. Signing in with Google creates an account the first time."
            : "Use the account you registered."}{" "}
          Membership in an organization is by invitation, not by signup.
        </p>
      </CardHeader>
      <CardContent className="flex flex-col gap-6">
        <PasswordSignInForm />
        {offersGoogle && (
          <>
            <div className="flex items-center gap-3">
              <span aria-hidden className="h-px flex-1 bg-border" />
              <span className="text-xs text-muted-foreground">or</span>
              <span aria-hidden className="h-px flex-1 bg-border" />
            </div>
            <div className="flex flex-col items-center gap-3">
              <GoogleSignInButton />
            </div>
          </>
        )}
      </CardContent>
      <CardFooter className="flex-col gap-2 text-xs text-muted-foreground">
        <p>
          Holding an invitation and no account yet?{" "}
          <Link
            href="/register"
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
