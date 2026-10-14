import type { Metadata } from "next";
import Link from "next/link";
import { Building2 } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";

import { ApiUnreachable } from "../_components/api-unreachable";
import { listConsoleOrgs } from "../_components/console-orgs";
import { EmptyState } from "../_components/empty-state";
import { PageHeader } from "../_components/page-header";

/** Head metadata for the organizations route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Organizations — garam" };

/** The console's first screen: every organization the caller belongs to. */
export default async function OrgsPage() {
  const orgs = await listConsoleOrgs();

  if (orgs.status === "unreachable") {
    return (
      <>
        <PageHeader
          title="Organizations"
          description="The organizations you belong to."
        />
        <ApiUnreachable subject="your organizations" />
      </>
    );
  }

  return (
    <>
      <PageHeader
        title="Organizations"
        description="The organizations you belong to."
      >
        <Button asChild>
          <Link href="/console/orgs/new">New organization</Link>
        </Button>
      </PageHeader>

      {orgs.orgs.length === 0 ? (
        <EmptyState
          icon={Building2}
          title="No organizations yet"
          description="You are not a member of any organization yet. Create one to get started."
        />
      ) : (
        <ul className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {orgs.orgs.map((org) => (
            <li key={org.slug}>
              <Link href={`/console/orgs/${org.slug}`} className="block">
                <Card className="gap-1 p-5 transition-colors hover:border-ring">
                  <CardTitle>{org.name}</CardTitle>
                  <CardDescription className="font-mono text-xs">
                    {org.slug}
                  </CardDescription>
                </Card>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </>
  );
}
