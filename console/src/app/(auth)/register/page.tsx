import type { Metadata } from "next";
import Link from "next/link";
import { redirect } from "next/navigation";

import {
  Card,
  CardContent,
  CardFooter,
  CardHeader,
} from "@garam/design/ui/card";
import { PasswordRegisterForm, getSession } from "@/features/auth";
import {
  RETURN_PARAM,
  safeReturnPath,
  withReturnPath,
} from "@/lib/return-path";

import { ApiUnreachableCard } from "../_components/api-unreachable-card";
import {
  InvitationContext,
  readInvitationContext,
} from "../_components/invitation-context";

/**
 * Head metadata for the register route; Next.js reads it by name. The
 * return path can hold an invitation token, so no link followed from
 * here sends it on as a referrer.
 */
export const metadata: Metadata = {
  title: "Create an account — garam",
  referrer: "no-referrer",
};

type Props = {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
};

/**
 * Where an invitation becomes an account. Registration is
 * invitation-only (ADR-0041). Reached from an invitation link, the
 * return path carries its token, so the page names the organization
 * and role and fills the token in; reached without one, it asks for
 * the token. An invitation that can no longer be used is said instead
 * of a form that would be refused. A visitor already holding a session
 * is sent on: registering again would mint a second account and sign
 * this browser into it.
 */
export default async function RegisterPage({ searchParams }: Props) {
  const returnPath = safeReturnPath((await searchParams)[RETURN_PARAM]);
  const session = await getSession();
  if (session.status === "signed-in") {
    redirect(returnPath ?? "/console");
  }
  if (session.status === "unreachable") {
    return <ApiUnreachableCard subject="the registration form" />;
  }
  const invitation = await readInvitationContext(returnPath);

  return (
    <Card className="w-full max-w-sm [--card-spacing:--spacing(6)]">
      <CardHeader className="gap-2">
        <h1 className="text-xl tracking-tight">
          {invitation?.usable
            ? `Create an account to join ${invitation.orgName}`
            : "Create your garam account"}
        </h1>
        <p className="text-pretty text-muted-foreground">
          {invitation?.usable
            ? "Your invitation is already filled in. Choose a username and a password, and you'll join as soon as the account exists."
            : "Registration takes an invitation: organizations grow by invitation, not by signup."}{" "}
          The account this creates is garam&apos;s own, separate from any other
          account you hold.
        </p>
      </CardHeader>
      <CardContent className="flex flex-col gap-6">
        {invitation !== null && <InvitationContext context={invitation} />}
        {invitation?.usable === false ? null : (
          <PasswordRegisterForm
            invitationToken={invitation?.usable ? invitation.token : null}
          />
        )}
      </CardContent>
      <CardFooter className="flex-col gap-2 text-muted-foreground">
        <p>
          Already registered?{" "}
          <Link
            href={withReturnPath("/login", returnPath)}
            // The return path can hold an invitation token: a prefetch
            // would send it in a request nobody asked for.
            prefetch={false}
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
