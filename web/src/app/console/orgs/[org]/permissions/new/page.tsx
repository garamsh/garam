import type { Metadata } from "next";

import { PageHeader } from "../../../../_components/page-header";
import { CreatePolicyForm } from "./_components/create-policy-form";

/** Head metadata for the new-policy route; Next.js reads it by name. */
export const metadata: Metadata = { title: "New policy — garam" };

type Params = { params: Promise<{ org: string }> };

/** Creates a policy; the draft garam records opens its own page. */
export default async function NewPolicyPage({ params }: Params) {
  const { org } = await params;
  return (
    <>
      <PageHeader
        title="New policy"
        description="A policy starts as a draft and grants nothing until it is activated."
      />
      <CreatePolicyForm org={org} />
    </>
  );
}
