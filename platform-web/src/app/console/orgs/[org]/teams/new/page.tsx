import type { Metadata } from "next";

import { Banner } from "@garam/design/components/banner";
import { checkPermissions } from "@/lib/permission-check";

import { SettingsPage } from "../../../../_components/templates/settings-page";
import { CreateTeamForm } from "./_components/create-team-form";

/** Head metadata for the create-team route; Next.js reads it by name. */
export const metadata: Metadata = { title: "New team — garam" };

type Params = { params: Promise<{ org: string }> };

/**
 * Creates a team and opens it. Members are added on the team's own
 * page, where the people and agents they can be chosen from are read.
 * A caller the server answers may not create one is told so in place
 * of a form it would refuse; any other answer renders the form.
 */
export default async function NewTeamPage({ params }: Params) {
  const { org } = await params;
  const permissions = await checkPermissions(org, {
    create: { action: "group:create" },
  });

  return (
    <SettingsPage
      title="New team"
      description="You add its members on the next page."
      sections={[
        {
          id: "name",
          title: "Name",
          content:
            permissions.get("create") === "denied" ? (
              <Banner tone="info" title="No policy lets you create teams" />
            ) : (
              <CreateTeamForm org={org} />
            ),
        },
      ]}
    />
  );
}
