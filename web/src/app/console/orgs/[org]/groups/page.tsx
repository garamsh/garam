import type { Metadata } from "next";
import { UsersRound } from "lucide-react";

import { listGroups, orgNotFound } from "@/features/orgs/api/orgs";

import { EmptyState } from "../../../_components/empty-state";
import { PageHeader } from "../../../_components/page-header";
import { ResourceList } from "../_components/resource-list";

export const metadata: Metadata = { title: "Groups — garam" };

type Params = { params: Promise<{ org: string }> };

export default async function GroupsPage({ params }: Params) {
  const { org } = await params;
  const groups = await listGroups(org).catch(orgNotFound);

  return (
    <>
      <PageHeader
        title="Groups"
        description="Bundles of members this organization grants access through."
      />
      {groups.length === 0 ? (
        <EmptyState
          icon={UsersRound}
          title="No groups"
          description="This organization has no groups."
        />
      ) : (
        <ResourceList resources={groups} />
      )}
    </>
  );
}
