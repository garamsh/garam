import type { Metadata } from "next";
import Link from "next/link";
import { redirect } from "next/navigation";

import {
  Card,
  CardContent,
  CardFooter,
  CardHeader,
} from "@/components/ui/card";
import { getSession } from "@/features/auth/api/get-session";
import { listProviders } from "@/features/auth/api/list-providers";
import { DevSignInButton } from "@/features/auth/components/dev-sign-in-button";
import { GoogleSignInButton } from "@/features/auth/components/google-sign-in-button";

export const metadata: Metadata = { title: "Sign in — garam" };

export default async function LoginPage() {
  if (await getSession()) {
    redirect("/console");
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
