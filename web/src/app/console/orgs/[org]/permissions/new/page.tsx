import type { Metadata } from "next";

import { checkPermissions } from "@/lib/permission-check";

import { SettingsPage } from "../../../../_components/templates/settings-page";
import { CreatePolicyRefused } from "./_components/create-policy-refused";
import { CreatePolicyForm } from "./_components/create-policy-form";

/** Head metadata for the new-policy route; Next.js reads it by name. */
export const metadata: Metadata = { title: "New policy — garam" };

type Params = { params: Promise<{ org: string }> };

/**
 * Creates a policy; the draft garam records opens its own page. The
 * server is asked whether the caller may create one, and only its
 * explicit deny takes the form away (ADR-0103).
 */
export default async function NewPolicyPage({ params }: Params) {
  const { org } = await params;
  const permissions = await checkPermissions(org, {
    create: { action: "policy:create" },
  });
  return (
    <SettingsPage
      title="New policy"
      description="A policy starts as a draft and grants nothing until it is activated."
      sections={[
        {
          id: "policy",
          title: "Policy",
          description:
            "Its name and first version. You can publish new versions once it exists.",
          content:
            permissions.get("create") === "denied" ? (
              <CreatePolicyRefused />
            ) : (
              <CreatePolicyForm org={org} />
            ),
        },
      ]}
    />
  );
}
