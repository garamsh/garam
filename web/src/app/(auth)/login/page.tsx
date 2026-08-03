import type { Metadata } from "next";
import { redirect } from "next/navigation";

import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { getSession } from "@/features/auth/api/get-session";
import { GoogleSignInButton } from "@/features/auth/components/google-sign-in-button";

export const metadata: Metadata = { title: "Sign in — garam" };

export default async function LoginPage() {
  if (await getSession()) {
    redirect("/console");
  }
  return (
    <Card className="w-full max-w-sm">
      <CardHeader>
        <h1 className="font-heading text-2xl font-semibold">Sign in to garam</h1>
        <p className="text-sm text-muted-foreground">
          Use the Google account your organization invited.
        </p>
      </CardHeader>
      <CardContent className="flex justify-center">
        <GoogleSignInButton />
      </CardContent>
    </Card>
  );
}
