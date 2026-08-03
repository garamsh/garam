import Link from "next/link";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardTitle } from "@/components/ui/card";

/**
 * Every claim here has to exist in the product: organization, group and
 * project management, org roles, policy documents, Google sign-in.
 */
const capabilities = [
  {
    title: "Organizations and members",
    body: "Bring people into an organization by invitation and give each one an org role — owner, admin, or member.",
  },
  {
    title: "Groups and projects",
    body: "File the work: groups and projects live under an organization, and members are added to each of them.",
  },
  {
    title: "Policy-based authorization",
    body: "Access is decided from IAM-style policy documents, stored per organization and versioned append-only.",
  },
];

export default function LandingPage() {
  return (
    <>
      <section className="border-b border-border/60 bg-linear-to-b from-muted/60 to-background">
        <div className="mx-auto flex w-full max-w-6xl flex-col items-start gap-6 px-6 py-24 md:py-32">
          <span className="rounded-full border px-3 py-1 text-xs font-medium tracking-wide text-muted-foreground uppercase">
            Identity and authorization
          </span>
          <h1 className="font-heading max-w-3xl text-4xl font-semibold tracking-tight text-balance md:text-6xl">
            Who belongs to your organization, and what they may do.
          </h1>
          <p className="max-w-2xl text-lg text-pretty text-muted-foreground">
            garam keeps the organizations, groups, and projects your work is
            filed under, the people that belong to them, and the policies that
            decide what each of them may do.
          </p>
          <div className="flex flex-wrap items-center gap-3 pt-2">
            <Button asChild size="lg">
              <Link href="/console">Open the console</Link>
            </Button>
            <span className="text-sm text-muted-foreground">
              Sign in with the Google account your organization invited.
            </span>
          </div>
        </div>
      </section>

      <section className="mx-auto w-full max-w-6xl px-6 py-20">
        <ul className="grid gap-6 md:grid-cols-3">
          {capabilities.map((capability) => (
            <li key={capability.title}>
              <Card className="h-full gap-3 p-6">
                <CardTitle className="text-base">{capability.title}</CardTitle>
                <CardContent className="p-0 text-sm text-muted-foreground">
                  {capability.body}
                </CardContent>
              </Card>
            </li>
          ))}
        </ul>
      </section>

      <section className="border-t border-border/60 bg-muted/40">
        <div className="mx-auto grid w-full max-w-6xl gap-8 px-6 py-20 md:grid-cols-2 md:items-center">
          <div className="flex flex-col gap-4">
            <h2 className="font-heading text-2xl font-semibold tracking-tight md:text-3xl">
              One identifier for everything
            </h2>
            <p className="text-muted-foreground">
              Users, organizations, groups, projects, and policies all carry a
              GRN. One name shape means one way to ask who a subject is and
              what a policy is written about.
            </p>
          </div>
          <code className="overflow-x-auto rounded-lg border bg-background p-6 font-mono text-sm whitespace-nowrap">
            grn:&lt;organization&gt;:&lt;project&gt;:&lt;service_type&gt;:&lt;identifier&gt;
          </code>
        </div>
      </section>

      <section className="mx-auto w-full max-w-6xl px-6 py-24">
        <div className="flex flex-col items-center gap-4 rounded-2xl border px-6 py-16 text-center">
          <h2 className="font-heading text-2xl font-semibold tracking-tight md:text-3xl">
            In the console today
          </h2>
          <p className="max-w-xl text-pretty text-muted-foreground">
            The organizations you belong to, and for each one its groups,
            projects, and members with their roles.
          </p>
          <Button asChild size="lg" className="mt-2">
            <Link href="/console">Open the console</Link>
          </Button>
        </div>
      </section>
    </>
  );
}
