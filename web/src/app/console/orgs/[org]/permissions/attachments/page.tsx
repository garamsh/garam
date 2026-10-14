import type { Metadata } from "next";

import { Banner } from "@/components/banner";
import {
  KIND_LABEL,
  getOrg,
  listGroupMembers,
  listPolicies,
  listPolicyAttachments,
  lookupAgentDefinitions,
  lookupAgents,
  lookupGroups,
  lookupOrgMembers,
  slugFromGrn,
  type Catalog,
  type Policy,
} from "@/features/orgs";
import {
  MAX_PERMISSION_CHECKS,
  checkPermissions,
  type PermissionCheck,
} from "@/lib/permission-check";

import { ApiUnreachable } from "../../../../_components/api-unreachable";
import { orgHref } from "../../../../_components/navigation";
import { ListPage } from "../../../../_components/templates/list-page";
import { PermissionsViews } from "../_components/permissions-views";
import { PoliciesRefused } from "../_components/policies-refused";
import {
  KIND_PLURAL,
  inspectSubject,
  reachOf,
  subjectLabel,
  listSubjects,
  partlyListedAgents,
  resolveSubjectQuery,
  subjectChoices,
  subjectQuery,
  type Catalogs,
  type PolicyScope,
  type TeamMembership,
} from "../_components/subjects";
import { ScopeTable, type ScopeRow } from "./_components/scope-table";
import { SubjectInspection } from "./_components/subject-inspection";
import { SubjectPicker } from "./_components/subject-picker";

/** Head metadata for the attachments route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Attachments — garam" };

type Props = {
  params: Promise<{ org: string }>;
  searchParams: Promise<{ subject?: string | string[] }>;
};

/** The checks the subject view asks: attach and detach, per policy. */
type PolicyAction = "attach" | "detach";

/**
 * The page's one permission check, for the subject view's writes. A
 * request holds at most `MAX_PERMISSION_CHECKS` pairs, so past that
 * the policies beyond it are not asked and render their controls,
 * which the server still decides (ADR-0103).
 */
function policyChecks(
  policies: readonly Policy[],
): Record<string, PermissionCheck> {
  const checks: Record<string, PermissionCheck> = {};
  for (const policy of policies.slice(0, MAX_PERMISSION_CHECKS / 2)) {
    const resource = { type: "policy", id: slugFromGrn(policy.grn) } as const;
    checks[`attach ${policy.grn}`] = { action: "policy:attach", resource };
    checks[`detach ${policy.grn}`] = { action: "policy:detach", resource };
  }
  return checks;
}

/**
 * The Attachments view of Permissions: whom each policy is attached
 * to, and for one chosen team, person or agent, which policies name it
 * and from where (ADR-0073).
 *
 * garam has no read of a subject's policies, so the page reads every
 * policy's attachments and, for a person or an agent, every team's
 * members. A list garam refuses is unchecked, never empty: a refused
 * attachment list is not an organization-wide policy, and a refused
 * member list is not the absence of a team.
 */
export default async function AttachmentsPage({ params, searchParams }: Props) {
  const { org } = await params;
  const { subject: query } = await searchParams;
  const [organization, policies, teams, people, agents, definitions] =
    await Promise.all([
      getOrg(org),
      listPolicies(org),
      lookupGroups(org),
      lookupOrgMembers(org),
      lookupAgents(org),
      lookupAgentDefinitions(org),
    ]);
  const page = (children: React.ReactNode, count?: number) => (
    <ListPage
      title="Permissions"
      description="Which policies are attached to which teams, people and agents."
      count={count}
      filters={<PermissionsViews org={org} current="attachments" />}
    >
      {children}
    </ListPage>
  );

  if (
    organization.status === "unreachable" ||
    policies.status === "unreachable" ||
    teams.status === "unreachable" ||
    people.status === "unreachable" ||
    agents.status === "unreachable" ||
    definitions.status === "unreachable"
  ) {
    return page(<ApiUnreachable subject="this organization's attachments" />);
  }
  if (policies.status === "forbidden") return page(<PoliciesRefused />);

  const catalogs: Catalogs = { teams, people, agents, definitions };
  const partlyListed = partlyListedAgents(catalogs);
  const lookup = resolveSubjectQuery(
    typeof query === "string" ? query : undefined,
    catalogs,
  );
  const inspectsMembership =
    lookup.status === "found" && lookup.subject.kind !== "team";
  const [scopeReads, membershipReads, permissions] = await Promise.all([
    Promise.all(
      policies.data.map((policy) =>
        listPolicyAttachments(org, slugFromGrn(policy.grn)),
      ),
    ),
    inspectsMembership && teams.status === "ok"
      ? Promise.all(
          teams.data.map((team) =>
            listGroupMembers(org, slugFromGrn(team.grn)),
          ),
        )
      : Promise.resolve([]),
    checkPermissions(
      org,
      lookup.status === "found" ? policyChecks(policies.data) : {},
    ),
  ]);
  if (
    scopeReads.some((read) => read.status === "unreachable") ||
    membershipReads.some((read) => read.status === "unreachable")
  ) {
    return page(<ApiUnreachable subject="this organization's attachments" />);
  }

  const scopes: PolicyScope[] = policies.data.map((policy, index) => {
    const read = scopeReads[index];
    return {
      policy,
      attachments:
        read.status === "ok"
          ? { status: "ok", data: read.data }
          : { status: "forbidden" },
    };
  });
  // Read only for a person or an agent; a team belongs to no team.
  const memberships: Catalog<TeamMembership> =
    teams.status === "forbidden"
      ? { status: "forbidden" }
      : {
          status: "ok",
          data: membershipReads.map((read, index) => ({
            team: teams.data[index],
            members:
              read.status === "ok"
                ? { status: "ok", data: read.data }
                : { status: "forbidden" },
          })),
        };
  const base = orgHref(org, "permissions");
  const policyHref = (policy: string) => `${base}/${policy}`;
  const subjectHref = (value: string) =>
    `${base}/attachments?subject=${encodeURIComponent(value)}`;
  const rows: ScopeRow[] = scopes.map(({ policy, attachments }) => {
    const reach = reachOf(attachments, catalogs);
    const identifier = slugFromGrn(policy.grn);
    return {
      identifier,
      name: policy.name,
      href: policyHref(identifier),
      status: policy.status,
      reach:
        reach.kind === "unknown"
          ? { kind: "unknown" }
          : reach.kind === "org-wide"
            ? { kind: policy.status === "active" ? "everyone" : "none" }
            : {
                kind: "attached",
                subjects: reach.subjects.map((subject) => ({
                  key: subject.grn,
                  text: `${subjectLabel(subject)} (${KIND_LABEL[subject.kind]})`,
                  href:
                    subject.lookup === "found" && subject.kind !== "unknown"
                      ? subjectHref(`${subject.kind}:${subject.identifier}`)
                      : null,
                })),
              },
    };
  });

  return page(
    <>
      {partlyListed.length > 0 && (
        <Banner tone="info" title="Some agents may be missing">
          This organization&apos;s {partlyListed.join(" and ")} are not listed
          to you.
        </Banner>
      )}
      <SubjectPicker
        action={`${base}/attachments`}
        groups={subjectChoices(listSubjects(catalogs), subjectQuery)}
        current={lookup.status === "found" ? subjectQuery(lookup.subject) : ""}
      />
      {lookup.status === "found" ? (
        <SubjectInspection
          org={org}
          subject={lookup.subject}
          inspection={inspectSubject(lookup.subject, scopes, memberships)}
          attachable={scopes.flatMap(({ policy, attachments }) =>
            attachments.status === "ok" &&
            !attachments.data.some(
              (attachment) => attachment.subjectGrn === lookup.subject.grn,
            )
              ? [{ policy, unattached: attachments.data.length === 0 }]
              : [],
          )}
          policyHref={policyHref}
          subjectHref={subjectHref}
          may={(action: PolicyAction, policy) =>
            permissions.get(`${action} ${policy.grn}`) !== "denied"
          }
        />
      ) : lookup.status === "missing" ? (
        <Banner tone="danger" title="Not found">
          No team, person or agent of this organization matches this address.
        </Banner>
      ) : lookup.status === "refused" ? (
        <Banner tone="info" title="Not shown">
          This organization&apos;s {KIND_PLURAL[lookup.kind]} are not listed to
          you, so this one cannot be shown.
        </Banner>
      ) : (
        <ScopeTable rows={rows} />
      )}
    </>,
    policies.data.length,
  );
}
