import type { Metadata } from "next";

import { Banner } from "@garam/design/components/banner";
import {
  lookupAgents,
  lookupGroups,
  lookupOrgMembers,
  lookupProjects,
  roleLinkChoices,
  type RefusableRead,
} from "@/features/orgs";
import { formatGrn } from "@/lib/grn";

import { SettingsPage } from "../../../../_components/templates/settings-page";
import { creatableKinds } from "../_components/policy-actions";
import { CreatePolicyForm } from "./_components/create-policy-form";

/** Head metadata for the create-policy route; Next.js reads it by name. */
export const metadata: Metadata = { title: "New policy — garam" };

type Params = { params: Promise<{ org: string }> };

/** What a read holds, or null where it was refused or not read. */
function held<T>(read: RefusableRead<T>): T | null {
  return read.status === "ok" ? read.data : null;
}

/**
 * Creates a role link, a guardrail or a permit and opens it as the
 * draft it is stored as (ADR-0173). The server is asked once, on the
 * organization, which kinds the caller may create, and only an
 * explicit deny withdraws one (ADR-0103); the policy service enforces
 * the write either way. The role form's choices are read from the
 * organization's teams, agents, people and projects.
 */
export default async function NewPolicyPage({ params }: Params) {
  const { org } = await params;
  const [offered, teams, agents, members, projects] = await Promise.all([
    creatableKinds(org),
    lookupGroups(org),
    lookupAgents(org),
    lookupOrgMembers(org),
    lookupProjects(org),
  ]);

  return (
    <SettingsPage
      title="New policy"
      description="It is stored as a draft and decides nothing until you activate it."
      sections={[
        {
          id: "policy",
          title: "Policy",
          content:
            offered.length === 0 ? (
              <Banner tone="info" title="No policy lets you create policies" />
            ) : (
              <CreatePolicyForm
                org={org}
                orgGrn={formatGrn("root", "iam", "org", org)}
                offered={offered}
                choices={roleLinkChoices({
                  teams: held(teams),
                  agents: held(agents),
                  members: held(members),
                  projects: held(projects),
                })}
              />
            ),
        },
      ]}
    />
  );
}
