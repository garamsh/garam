import type { Metadata } from "next";
import Link from "next/link";
import { Plus } from "lucide-react";

import { Button } from "@/components/ui/button";
import { getOrg, listPolicies, slugFromGrn } from "@/features/orgs";
import { checkPermissions } from "@/lib/permission-check";

import { ApiUnreachable } from "../../../_components/api-unreachable";
import { NEW_ITEM, orgHref } from "../../../_components/navigation";
import { ListPage } from "../../../_components/templates/list-page";
import { PermissionsViews } from "./_components/permissions-views";
import { RefusedRead } from "../../../_components/refused-read";
import { PolicyTable } from "./_components/policy-table";

/** Head metadata for the permissions route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Permissions — garam" };

type Params = { params: Promise<{ org: string }> };

/**
 * The Policies view of Permissions: the organization's policies, each
 * opening a page of its own (ADR-0073). The organization is read for
 * itself, so one the caller cannot read answers 404 (ADR-0011), and a
 * refused policy list inside one it reads says so. Creating is offered
 * unless the server denies it (ADR-0103).
 */
export default async function PermissionsPage({ params }: Params) {
  const { org } = await params;
  const base = orgHref(org, "permissions");
  const [organization, policies, permissions] = await Promise.all([
    getOrg(org),
    listPolicies(org),
    checkPermissions(org, { create: { action: "policy:create" } }),
  ]);
  const listed = policies.status === "ok" ? policies.data : null;
  const create = permissions.get("create") !== "denied" && (
    <Button asChild>
      <Link href={`${base}/${NEW_ITEM}`}>
        <Plus aria-hidden />
        Create policy
      </Link>
    </Button>
  );

  return (
    <ListPage
      title="Permissions"
      count={listed?.length}
      primaryAction={create}
      filters={<PermissionsViews org={org} current="policies" />}
    >
      {organization.status === "unreachable" ||
      policies.status === "unreachable" ? (
        <ApiUnreachable subject="this organization's policies" />
      ) : listed === null ? (
        <RefusedRead subject="this organization's policies" />
      ) : (
        <PolicyTable
          emptyAction={create}
          rows={listed.map((policy) => {
            const identifier = slugFromGrn(policy.grn);
            return { policy, identifier, href: `${base}/${identifier}` };
          })}
        />
      )}
    </ListPage>
  );
}
