import type { Metadata } from "next";
import Link from "next/link";
import { redirect } from "next/navigation";

import {
  Card,
  CardContent,
  CardFooter,
  CardHeader,
} from "@/components/ui/card";
import { PasswordRegisterForm, getSession } from "@/features/auth";

import { ApiUnreachableCard } from "../_components/api-unreachable-card";

/** Head metadata for the register route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Create an account — garam" };

/**
 * Where an invitation becomes an account. Registration is
 * invitation-only (ADR-0041), so the page asks for the token rather
 * than offering an open form. A visitor already holding a session is
 * sent to the console: registering again would mint a second account
 * and sign this browser into it.
 */
export default async function RegisterPage() {
  const session = await getSession();
  if (session.status === "signed-in") {
    redirect("/console");
  }
  if (session.status === "unreachable") {
    return <ApiUnreachableCard subject="the registration form" />;
  }
  return (
    <Card className="w-full max-w-sm [--card-spacing:--spacing(6)]">
      <CardHeader className="gap-2">
        <h1 className="font-heading text-2xl font-semibold tracking-tight">
          Create your garam account
        </h1>
        <p className="text-sm text-pretty text-muted-foreground">
          Registration takes an invitation: organizations grow by invitation,
          not by signup. The account this creates is garam&apos;s own, separate
          from any Google account you hold.
        </p>
      </CardHeader>
      <CardContent>
        <PasswordRegisterForm />
      </CardContent>
      <CardFooter className="flex-col gap-2 text-xs text-muted-foreground">
        <p>
          Already registered?{" "}
          <Link
            href="/login"
            className="text-foreground underline underline-offset-4"
          >
            Sign in
          </Link>
        </p>
        <Link href="/" className="transition-colors hover:text-foreground">
          Back to the home page
        </Link>
      </CardFooter>
    </Card>
  );
}
