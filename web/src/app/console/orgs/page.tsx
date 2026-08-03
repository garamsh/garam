import type { Metadata } from "next";
import Link from "next/link";

import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { listOrgs } from "@/features/orgs/api/orgs";
import { slugFromGrn } from "@/features/orgs/grn";

export const metadata: Metadata = { title: "Organizations — garam" };

export default async function OrgsPage() {
  const orgs = await listOrgs();

  return (
    <section className="flex flex-col gap-6">
      <div className="flex flex-col gap-1">
        <h1 className="font-heading text-2xl font-semibold">Organizations</h1>
        <p className="text-sm text-muted-foreground">
          The organizations you belong to.
        </p>
      </div>

      {orgs.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          You are not a member of any organization yet.
        </p>
      ) : (
        <ul className="grid gap-4 md:grid-cols-2">
          {orgs.map((org) => {
            const slug = slugFromGrn(org.grn);
            return (
              <li key={org.grn}>
                <Link href={`/console/orgs/${slug}`} className="block">
                  <Card className="gap-2 p-5 transition-colors hover:border-ring">
                    <CardTitle>{org.name}</CardTitle>
                    <CardDescription className="font-mono text-xs">
                      {org.grn}
                    </CardDescription>
                  </Card>
                </Link>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
