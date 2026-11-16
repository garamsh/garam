import type { Metadata } from "next";

import { getOrg, listIamPolicies } from "@/features/orgs";

import { ApiUnreachable } from "../../../_components/api-unreachable";
import { orgHref } from "../../../_components/navigation";
import { ListPage } from "../../../_components/templates/list-page";
import { RefusedRead } from "../../../_components/refused-read";
import { PolicyTable } from "./_components/policy-table";

/** Head metadata for the permissions route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Permissions — garam" };

type Params = { params: Promise<{ org: string }> };

/**
 * The organization's policies: its role links, guardrails and permits,
 * each opening a page of its own (ADR-0169). The organization is read
 * for itself, so one the caller cannot read answers 404 (ADR-0011), and
 * a refused policy list inside one it reads says so. Creating one is
 * not offered here yet, and the page says where it is done.
 */
export default async function PermissionsPage({ params }: Params) {
  const { org } = await params;
  const base = orgHref(org, "permissions");
  const [organization, policies] = await Promise.all([
    getOrg(org),
    listIamPolicies(org),
  ]);
  const listed = policies.status === "ok" ? policies.data : null;

  return (
    <ListPage
      title="Permissions"
      count={listed?.length}
      primaryAction={
        <p className="text-muted-foreground">
          A policy is created through the API or the CLI.
        </p>
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
