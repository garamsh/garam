import type { Metadata } from "next";
import { Handshake } from "lucide-react";

import { getOrg, lookupOrgConsents, slugFromGrn } from "@/features/orgs";
import { checkPermissions } from "@/lib/permission-check";

import { ApiUnreachable } from "../../../_components/api-unreachable";
import { EmptyState } from "../../../_components/empty-state";
import { RefusedRead } from "../../../_components/refused-read";
import { ListPage } from "../../../_components/templates/list-page";
import { ConsentCard } from "./_components/consent-card";
import { GrantConsentDialog } from "./_components/grant-consent-dialog";

/** Head metadata for the consents route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Consents — garam" };

type Params = { params: Promise<{ org: string }> };

const DESCRIPTION =
  "The organizations this one exchanges messages with. A relation is a pair: a message crosses only while both organizations hold their half, and either side can withdraw its own.";

/**
 * The organizations this one may exchange messages with, one card per
 * pair, and the dialog that grants this organization's half of a new
 * one. Reading them needs `consent:read`, which not every member
 * holds, so a refused list is said rather than shown as none; the
 * organization is read beside it so that a caller who is not a member
 * is still answered not-found (ADR-0011).
 *
 * The page asks the server once whether the caller may grant and
 * withdraw, and leaves a control out only on an explicit deny
 * (ADR-0103).
 */
export default async function ConsentsPage({ params }: Params) {
  const { org } = await params;
  const [organization, consents, permissions] = await Promise.all([
    getOrg(org),
    lookupOrgConsents(org),
    checkPermissions(org, {
      grant: { action: "consent:grant" },
      withdraw: { action: "consent:withdraw" },
    }),
  ]);
  const mayGrant = permissions.get("grant") !== "denied";
  const mayWithdraw = permissions.get("withdraw") !== "denied";

  if (
    organization.status === "unreachable" ||
    consents.status === "unreachable"
  ) {
    return (
      <ListPage title="Consents" description={DESCRIPTION}>
        <ApiUnreachable subject="this organization's consents" />
      </ListPage>
    );
  }

  return (
    <ListPage
      title="Consents"
      description={DESCRIPTION}
      count={consents.status === "ok" ? consents.data.length : undefined}
      primaryAction={mayGrant ? <GrantConsentDialog org={org} /> : undefined}
    >
      {consents.status === "forbidden" ? (
        <RefusedRead subject="this organization's consent relations" />
      ) : consents.data.length === 0 ? (
        <EmptyState
          icon={Handshake}
          title="No consent relations"
        />
      ) : (
        <ul className="grid gap-4 lg:grid-cols-2">
          {consents.data.map((consent) => (
            <li key={consent.orgGrn} className="flex">
              <ConsentCard
                org={org}
                other={slugFromGrn(consent.orgGrn)}
                grantedHere={consent.grantedHere}
                grantedThere={consent.grantedThere}
                stands={consent.stands}
                mayGrant={mayGrant}
                mayWithdraw={mayWithdraw}
              />
            </li>
          ))}
        </ul>
      )}
    </ListPage>
  );
}
