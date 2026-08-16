import type { Metadata } from "next";
import Link from "next/link";
import { FolderGit2, UserRound, UsersRound } from "lucide-react";

import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import {
  getOrg,
  listGroups,
  listOrgMembers,
  listProjects,
  orgNotFound,
} from "@/features/orgs/api/orgs";

import { PageHeader } from "../../_components/page-header";
import { orgHref } from "../../_components/navigation";

type Params = { params: Promise<{ org: string }> };

export async function generateMetadata({ params }: Params): Promise<Metadata> {
  const { org } = await params;
  return { title: `${org} — garam` };
}

export default async function OrgPage({ params }: Params) {
  const { org: slug } = await params;
  const [org, groups, projects, members] = await Promise.all([
    getOrg(slug).catch(orgNotFound),
    listGroups(slug),
    listProjects(slug),
    listOrgMembers(slug),
  ]);

  const counts = [
    { segment: "groups", label: "Groups", icon: UsersRound, of: groups.length },
    {
      segment: "projects",
      label: "Projects",
      icon: FolderGit2,
      of: projects.length,
    },
    {
      segment: "members",
      label: "Members",
      icon: UserRound,
      of: members.length,
    },
  ];

  return (
    <>
      <PageHeader title={org.name} description="What this organization holds." />
      <ul className="grid gap-4 sm:grid-cols-3">
        {counts.map((count) => (
          <li key={count.segment}>
            <Link href={orgHref(slug, count.segment)} className="block">
              <Card className="gap-2 p-5 transition-colors hover:border-ring">
                <CardDescription className="flex items-center gap-2">
                  <count.icon className="size-4" />
                  {count.label}
                </CardDescription>
                <CardTitle className="text-3xl tabular-nums">
                  {count.of}
                </CardTitle>
              </Card>
            </Link>
          </li>
        ))}
      </ul>
    </>
  );
}
