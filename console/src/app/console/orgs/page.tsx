import type { Metadata } from "next";
import Link from "next/link";
import { Building2 } from "lucide-react";

import { Button } from "@garam/design/ui/button";
import { Card, CardDescription, CardTitle } from "@garam/design/ui/card";

import { ApiUnreachable } from "../_components/api-unreachable";
import { listConsoleOrgs } from "../_components/console-orgs";
import { EmptyState } from "../_components/empty-state";
import { ListPage } from "../_components/templates/list-page";

/** Head metadata for the organizations route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Organizations — garam" };

const TITLE = "Organizations";
/** The console's first screen: every organization the caller belongs to. */
export default async function OrgsPage() {
  const orgs = await listConsoleOrgs();

  if (orgs.status === "unreachable") {
    return (
      <ListPage title={TITLE}>
        <ApiUnreachable subject="your organizations" />
      </ListPage>
    );
  }

  return (
    <ListPage
      title={TITLE}

      primaryAction={
        <Button asChild>
          <Link href="/console/orgs/new">New organization</Link>
        </Button>
      }
    >
      {orgs.orgs.length === 0 ? (
        <EmptyState icon={Building2} title="No organizations yet" />
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
    </ListPage>
  );
}
