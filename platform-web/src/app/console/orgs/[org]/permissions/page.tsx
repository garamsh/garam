import type { Metadata } from "next";
import Link from "next/link";

import { Button } from "@garam/design/ui/button";
import { getOrg, listIamPolicies } from "@/features/orgs";

import { ApiUnreachable } from "../../../_components/api-unreachable";
import { NEW_ITEM, orgHref } from "../../../_components/navigation";
import { ListPage } from "../../../_components/templates/list-page";
import { RefusedRead } from "../../../_components/refused-read";
import { creatableKinds } from "./_components/policy-actions";
import { PolicyTable } from "./_components/policy-table";

/** Head metadata for the permissions route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Permissions — garam" };

type Params = { params: Promise<{ org: string }> };

/**
 * The organization's policies: its role links, guardrails and permits,
 * each opening a page of its own (ADR-0169). The organization is read
 * for itself, so one the caller cannot read answers 404 (ADR-0011), and
 * a refused policy list inside one it reads says so. Creating one is
 * offered unless the server denies every kind (ADR-0173).
 */
export default async function PermissionsPage({ params }: Params) {
  const { org } = await params;
  const base = orgHref(org, "permissions");
  const [organization, policies, creatable] = await Promise.all([
    getOrg(org),
    listIamPolicies(org),
    creatableKinds(org),
  ]);
  const listed = policies.status === "ok" ? policies.data : null;

  return (
    <ListPage
      title="Permissions"
      count={listed?.length}
      primaryAction={
        creatable.length > 0 && (
          <Button asChild>
            <Link href={`${base}/${NEW_ITEM}`}>Create policy</Link>
          </Button>
        )
      }
    >
      {organization.status === "unreachable" ||
      policies.status === "unreachable" ? (
        <ApiUnreachable subject="this organization's policies" />
      ) : listed === null ? (
        <RefusedRead subject="this organization's policies" />
      ) : (
        <PolicyTable
          rows={listed.map((policy) => ({
            policy,
            href: `${base}/${policy.id}`,
          }))}
        />
      )}
    </ListPage>
  );
}
