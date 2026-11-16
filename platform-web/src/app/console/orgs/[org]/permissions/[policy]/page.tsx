import type { Metadata } from "next";
import Link from "next/link";

import { Button } from "@garam/design/ui/button";
import { getIamPolicy, getOrg } from "@/features/orgs";
import { checkPermissions } from "@/lib/permission-check";

import { ApiUnreachable } from "../../../../_components/api-unreachable";
import { ItemCrumb } from "../../../../_components/item-crumb";
import { orgHref } from "../../../../_components/navigation";
import { Outcome, OutcomeProvider } from "../../../../_components/outcome";
import { PageHeader } from "../../../../_components/page-header";
import { RefusedRead } from "../../../../_components/refused-read";
import { DetailPage } from "../../../../_components/templates/detail-page";
import { WRITE_ACTION } from "../_components/policy-actions";
import { POLICY_KIND_LABEL } from "../_components/policy-kind";
import { PolicyStatus } from "../_components/policy-status";
import { ActivatePolicyDialog } from "./_components/activate-policy-dialog";
import { DeletePolicyDialog } from "./_components/delete-policy-dialog";
import { PolicyBody } from "./_components/policy-body";
import { VersionTable } from "./_components/version-table";

/** Head metadata for a policy's route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Policy — garam" };

type Params = { params: Promise<{ org: string; policy: string }> };

/** The actions `WRITE_ACTION` names, each once, on the policy. */
function askedOn(policy: string) {
  const actions = new Set(
    Object.values(WRITE_ACTION).flatMap(({ write, remove }) => [write, remove]),
  );
  return Object.fromEntries(
    [...actions].map((action) => [
      action,
      { action, resource: { type: "policy", id: policy } as const },
    ]),
  );
}

/** The page's header where no policy could be shown. */
function Unshown({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="Policy" />
      {children}
    </div>
  );
}

/**
 * One policy, on the Detail template: its default version's body, its
 * versions, and deleting it in the danger zone (ADR-0169). A guardrail
 * or a permit shows its Cedar text read-only, and a role link its role,
 * scope, principal and resource. A guardrail or a permit offers a new
 * version, written on a page of its own.
 *
 * The kind decides which action each write is taken as, so the server
 * is asked once for them all, on the policy, beside the read, and only
 * an explicit deny withdraws a control (ADR-0103). The policy service
 * enforces every write either way.
 */
export default async function PolicyPage({ params }: Params) {
  const { org, policy: slug } = await params;
  const [organization, read, permissions] = await Promise.all([
    getOrg(org),
    getIamPolicy(org, slug),
    checkPermissions(org, askedOn(slug)),
  ]);

  if (organization.status === "unreachable" || read.status === "unreachable") {
    return (
      <Unshown>
        <ApiUnreachable subject="this policy" />
      </Unshown>
    );
  }
  if (read.status === "forbidden") {
    return (
      <Unshown>
        <RefusedRead subject="this policy" />
      </Unshown>
    );
  }

  const { policy, versions } = read.data;
  const actions = WRITE_ACTION[policy.kind];
  const canWrite = permissions.get(actions.write) !== "denied";
  const canRemove = permissions.get(actions.remove) !== "denied";
  const active = policy.status === "active";
  const defaultVersion = versions.find(
    (version) => version.version === policy.defaultVersion,
  );

  return (
    <OutcomeProvider>
      <ItemCrumb label={policy.name} />
      <DetailPage
        name={policy.name}
        idLabel="Identifier"
        id={policy.id}
        status={<PolicyStatus status={policy.status} />}
        facts={[
          { term: "Kind", description: POLICY_KIND_LABEL[policy.kind] },
          {
            term: "Default version",
            description: (
              <span className="font-mono tabular-nums">
                v{policy.defaultVersion}
              </span>
            ),
          },
        ]}
        actions={
          <>
            <Outcome area="lifecycle" />
            {policy.kind !== "role-link" && canWrite && (
              <Button asChild variant="outline">
                <Link
                  href={`${orgHref(org, "permissions")}/${policy.id}/new-version`}
                >
                  New version
                </Link>
              </Button>
            )}
            {!active && canWrite && (
              <ActivatePolicyDialog
                org={org}
                policy={policy.id}
                policyName={policy.name}
                version={policy.defaultVersion}
              />
            )}
          </>
        }
        tabs={[
          {
            value: "definition",
            label: "Definition",
            content:
              defaultVersion === undefined ? (
                <p className="text-muted-foreground">
                  Its default version, v{policy.defaultVersion}, is not among
                  the versions it holds.
                </p>
              ) : (
                <PolicyBody body={defaultVersion.body} />
              ),
          },
          {
            value: "versions",
            label: `Versions (${versions.length})`,
            content: (
              <>
                <Outcome area="versions" />
                <VersionTable
                  rows={versions.map((version) => ({
                    version: version.version,
                    body: version.body,
                    author: version.createdBy,
                  }))}
                  context={{
                    org,
                    policy: policy.id,
                    defaultVersion: policy.defaultVersion,
                    defaultBody: defaultVersion?.body,
                    active,
                    canWrite,
                  }}
                />
              </>
            ),
          },
        ]}
        dangerZone={
          canRemove
            ? [
                {
                  title: "Delete this policy",
                  control: (
                    <DeletePolicyDialog
                      org={org}
                      policy={policy.id}
                      policyName={policy.name}
                    />
                  ),
                },
              ]
            : undefined
        }
      />
    </OutcomeProvider>
  );
}
