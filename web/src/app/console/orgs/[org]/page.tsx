import type { Metadata } from "next";
import Link from "next/link";

import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import {
  getOrg,
  listGroups,
  listOrgMembers,
  listProjects,
  orgNotFound,
} from "@/features/orgs/api/orgs";

import { PageHeader } from "../../_components/page-header";
import { CONSOLE_SECTIONS, orgHref } from "../../_components/navigation";

type Params = { params: Promise<{ org: string }> };

/** Titles the route from the URL: the org's name needs a read. */
export async function generateMetadata({ params }: Params): Promise<Metadata> {
  const { org } = await params;
  return { title: `${org} — garam` };
}

/** What the organization holds, counted, one card per section. */
export default async function OrgPage({ params }: Params) {
  const { org: slug } = await params;
  const [org, groups, projects, members] = await Promise.all([
    getOrg(slug).catch(orgNotFound),
    listGroups(slug),
    listProjects(slug),
    listOrgMembers(slug),
  ]);

  const counts = new Map([
    ["groups", groups.length],
    ["projects", projects.length],
    ["members", members.length],
  ]);
  // Label, icon and order come from the sidebar's own list, so a card
  // cannot drift from the section it links to.
  const cards = CONSOLE_SECTIONS.flatMap((section) => {
    const count = counts.get(section.segment);
    return count === undefined ? [] : [{ ...section, count }];
  });

  return (
    <>
      <PageHeader title={org.name} description="What this organization holds." />
      <ul className="grid gap-4 sm:grid-cols-3">
        {cards.map((card) => (
          <li key={card.segment}>
            <Link href={orgHref(slug, card.segment)} className="block">
              <Card className="gap-2 p-5 transition-colors hover:border-ring">
                <CardDescription className="flex items-center gap-2">
                  <card.icon className="size-4" />
                  {card.label}
                </CardDescription>
                <CardTitle className="text-3xl tabular-nums">
                  {card.count}
                </CardTitle>
              </Card>
            </Link>
          </li>
        ))}
      </ul>
    </>
  );
}
