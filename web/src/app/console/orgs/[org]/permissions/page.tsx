import type { Metadata } from "next";
import Link from "next/link";
import { ShieldCheck } from "lucide-react";

import { Button } from "@/components/ui/button";
import { getOrg, listPolicies } from "@/features/orgs";

import { ApiUnreachable } from "../../../_components/api-unreachable";
import { EmptyState } from "../../../_components/empty-state";
import { NEW_ITEM, orgHref } from "../../../_components/navigation";
import { PageHeader } from "../../../_components/page-header";
import { PermissionsViews } from "./_components/permissions-views";
import { PoliciesRefused } from "./_components/policies-refused";
import { PolicyList } from "./_components/policy-list";

/** Head metadata for the permissions route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Permissions — garam" };

type Params = { params: Promise<{ org: string }> };

/**
 * The Policies view of Permissions: the organization's policies, each
 * opening a page of its own (ADR-0073). The organization is read for
 * itself, so one the caller cannot read answers 404 (ADR-0011), and a
 * refused policy list inside one it reads says so.
 */
export default async function PermissionsPage({ params }: Params) {
  const { org } = await params;
  const [organization, policies] = await Promise.all([
    getOrg(org),
    listPolicies(org),
  ]);

  return (
    <>
      <PageHeader
        title="Permissions"
        description="Policies decide what teams, people and agents may do."
      >
        <Button asChild>
          <Link href={`${orgHref(org, "permissions")}/${NEW_ITEM}`}>
            Create policy
          </Link>
        </Button>
      </PageHeader>
      <PermissionsViews org={org} current="policies" />
      {organization.status === "unreachable" ||
      policies.status === "unreachable" ? (
        <ApiUnreachable subject="this organization's policies" />
      ) : policies.status === "forbidden" ? (
        <PoliciesRefused />
      ) : policies.data.length === 0 ? (
        <EmptyState
          icon={ShieldCheck}
          title="No policies"
          description="This organization has no policies."
        />
      ) : (
        <PolicyList org={org} policies={policies.data} />
      )}
    </>
  );
}
