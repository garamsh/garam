import type { Metadata } from "next";
import { UsersRound } from "lucide-react";

import { listGroups } from "@/features/orgs";

import { ApiUnreachable } from "../../../_components/api-unreachable";
import { EmptyState } from "../../../_components/empty-state";
import { PageHeader } from "../../../_components/page-header";
import { ResourceList } from "../_components/resource-list";

/** Head metadata for the groups route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Groups — garam" };

type Params = { params: Promise<{ org: string }> };

/** The organization's groups, or a note that it holds none. */
export default async function GroupsPage({ params }: Params) {
  const { org } = await params;
  const groups = await listGroups(org);

  return (
    <>
      <PageHeader
        title="Groups"
        description="Bundles of members this organization grants access through."
      />
      {groups.status === "unreachable" ? (
        <ApiUnreachable subject="this organization's groups" />
      ) : groups.data.length === 0 ? (
        <EmptyState
          icon={UsersRound}
          title="No groups"
          description="This organization has no groups."
        />
      ) : (
        <ResourceList resources={groups.data} />
      )}
    </>
  );
}
