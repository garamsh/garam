import type { Metadata } from "next";

import { getIamPolicy, getOrg, type IamPolicyKind } from "@/features/orgs";
import { checkPermissions } from "@/lib/permission-check";

import { ApiUnreachable } from "../../../../_components/api-unreachable";
import { ItemCrumb } from "../../../../_components/item-crumb";
import { Outcome, OutcomeProvider } from "../../../../_components/outcome";
import { PageHeader } from "../../../../_components/page-header";
import { RefusedRead } from "../../../../_components/refused-read";
import { DetailPage } from "../../../../_components/templates/detail-page";
import { POLICY_KIND_LABEL } from "../_components/policy-kind";
import { PolicyStatus } from "../_components/policy-status";
import { ActivatePolicyDialog } from "./_components/activate-policy-dialog";
import { DeletePolicyDialog } from "./_components/delete-policy-dialog";
import { PolicyBody } from "./_components/policy-body";
import { VersionTable } from "./_components/version-table";

/** Head metadata for a policy's route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Policy — garam" };

type Params = { params: Promise<{ org: string; policy: string }> };

/**
 * The action the policy service decides a write on each kind by, on
 * the organization: `write` for activating a policy or moving its
 * default version, `remove` for deleting it (ADR-0169).
 */
const WRITE_ACTION: Record<IamPolicyKind, { write: string; remove: string }> = {
  "role-link": { write: "iam:policy.attach", remove: "iam:policy.detach" },
  guardrail: { write: "iam:policy.update", remove: "iam:policy.delete" },
  permit: { write: "iam:permit.write", remove: "iam:policy.delete" },
};

/** Every action `WRITE_ACTION` names, each once, asked in one request. */
const ASKED = Object.fromEntries(
  Object.values(WRITE_ACTION)
    .flatMap(({ write, remove }) => [write, remove])
    .map((action) => [action, { action }]),
);

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
 * scope, principal and resource.
 *
 * The kind decides which action each write is taken as, so the server
 * is asked once for them all beside the read, and only an explicit deny
 * withdraws a control (ADR-0103). The policy service enforces every
 * write either way.
 */
export default async function PolicyPage({ params }: Params) {
  const { org, policy: slug } = await params;
  const [organization, read, permissions] = await Promise.all([
    getOrg(org),
    getIamPolicy(org, slug),
    checkPermissions(org, ASKED),
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
