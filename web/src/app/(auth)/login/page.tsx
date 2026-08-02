import type { Metadata } from "next";
import { redirect } from "next/navigation";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { getSession } from "@/features/auth/api/get-session";

export const metadata: Metadata = { title: "Sign in — garam" };

export default async function LoginPage() {
  if (await getSession()) {
    redirect("/orgs");
  }
  return (
    <Card className="w-full max-w-sm">
      <CardHeader>
        <h1 className="font-heading text-2xl font-semibold">Sign in to garam</h1>
        <p className="text-sm text-muted-foreground">
          Use the Google account your organization invited.
        </p>
      </CardHeader>
      <CardContent>
        {/* A plain link, not a fetch: the backend answers with a 302 to
            Google and sets the PKCE cookies on this origin. */}
        <Button asChild className="w-full">
          <a href="/api/auth/login/google">Continue with Google</a>
        </Button>
      </CardContent>
    </Card>
  );
}
