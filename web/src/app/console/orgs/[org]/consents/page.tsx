import type { Metadata } from "next";
import { Handshake } from "lucide-react";

import { Separator } from "@/components/ui/separator";
import { listOrgConsents, orgNotFound } from "@/features/orgs";

import { ApiUnreachable } from "../../../_components/api-unreachable";
import { EmptyState } from "../../../_components/empty-state";
import { PageHeader } from "../../../_components/page-header";
import { ConsentList } from "./_components/consent-list";
import { GrantConsentForm } from "./_components/grant-consent-form";

/** Head metadata for the consents route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Consents — garam" };

type Params = { params: Promise<{ org: string }> };

/**
 * The organizations this one may exchange messages with, and the form
 * that grants its half of one more relation.
 *
 * The read requires the admin role, so a member the API refuses is
 * answered as every other org-scoped refusal is — the section is not
 * theirs to see. Nothing here decides that; the API does.
 */
export default async function ConsentsPage({ params }: Params) {
  const { org } = await params;
  const consents = await listOrgConsents(org).catch(orgNotFound);

  return (
    <>
      <PageHeader
        title="Consents"
        description="The organizations this one exchanges messages with. A relation is a pair: a message crosses only while both organizations hold their half, and either side can withdraw its own."
      />
      {consents.status === "unreachable" ? (
        <ApiUnreachable subject="this organization's consents" />
      ) : consents.data.length === 0 ? (
        <EmptyState
          icon={Handshake}
          title="No consent relations"
          description="This organization holds no relation with another, so a message between one of its agents and an agent outside it is refused back to the sender."
        />
      ) : (
        <ConsentList org={org} consents={consents.data} />
      )}
      <Separator />
      <GrantConsentForm org={org} />
    </>
  );
}
