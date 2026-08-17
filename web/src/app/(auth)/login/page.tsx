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
  DevSignInButton,
  GoogleSignInButton,
  getSession,
  listProviders,
} from "@/features/auth";

/** Head metadata for the sign-in route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Sign in — garam" };

/**
 * The way in. A visitor who already holds a valid session is sent on
 * to the console instead of being offered the form again. An API that
 * did not answer is said out loud: every way in from here runs through
 * it, so offering the buttons would be offering a door that is shut.
 */
export default async function LoginPage() {
  const session = await getSession();
  if (session.status === "signed-in") {
    redirect("/console");
  }
  if (session.status === "unreachable") {
    return (
      <Card className="w-full max-w-sm [--card-spacing:--spacing(6)]">
        <CardHeader className="gap-2">
          <h1 className="font-heading text-2xl font-semibold tracking-tight">
            Cannot reach the API
          </h1>
          <p className="text-sm text-pretty text-muted-foreground">
            Signing in needs the garam API, and it is not answering. This is not
            a sign-in problem and not a permission problem: nothing here can
            tell whether you are signed in until the API is back.
          </p>
        </CardHeader>
        <CardFooter className="justify-center text-xs text-muted-foreground">
          <Link href="/" className="transition-colors hover:text-foreground">
            Back to the home page
          </Link>
        </CardFooter>
      </Card>
    );
  }
  const providers = await listProviders();
  return (
    <Card className="w-full max-w-sm [--card-spacing:--spacing(6)]">
      <CardHeader className="gap-2">
        <h1 className="font-heading text-2xl font-semibold tracking-tight">
          Sign in to garam
        </h1>
        <p className="text-sm text-pretty text-muted-foreground">
          Use the Google account your organization invited. Accounts are created
          by invitation, not by signup.
        </p>
      </CardHeader>
      <CardContent className="flex flex-col items-center gap-3">
        <GoogleSignInButton />
        {providers.includes("dev") && <DevSignInButton />}
      </CardContent>
      <CardFooter className="justify-center text-xs text-muted-foreground">
        <Link href="/" className="transition-colors hover:text-foreground">
          Back to the home page
        </Link>
      </CardFooter>
    </Card>
  );
}
