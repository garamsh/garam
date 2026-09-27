import type { Metadata } from "next";

import { PageHeader } from "../../../../_components/page-header";
import { CreateTeamForm } from "./_components/create-team-form";

/** Head metadata for the create-team route; Next.js reads it by name. */
export const metadata: Metadata = { title: "New team — garam" };

type Params = { params: Promise<{ org: string }> };

/**
 * Creates a team and opens it. Members are added on the team's own
 * page, where the people and agents they can be chosen from are read.
 */
export default async function NewTeamPage({ params }: Params) {
  const { org } = await params;

  return (
    <>
      <PageHeader
        title="New team"
        description="Name the team. You add its people and agents on the next page."
      />
      <CreateTeamForm org={org} />
    </>
  );
}
