import type { Metadata } from "next";
import { ShieldCheck } from "lucide-react";

import {
  getOrg,
  listGroupMembers,
  listPolicies,
  listPolicyAttachments,
  lookupAgents,
  lookupGroups,
  lookupOrgMembers,
  slugFromGrn,
} from "@/features/orgs";

import { ApiUnreachable } from "../../../../_components/api-unreachable";
import { EmptyState } from "../../../../_components/empty-state";
import { orgHref } from "../../../../_components/navigation";
import { PageHeader } from "../../../../_components/page-header";
import { PermissionsViews } from "../_components/permissions-views";
import { PoliciesRefused } from "../_components/policies-refused";
import {
  KIND_PLURAL,
  inspectSubject,
  listSubjects,
  resolveSubjectQuery,
  subjectChoices,
  subjectQuery,
  type Catalog,
  type Catalogs,
  type PolicyScope,
  type TeamMembership,
} from "../_components/subjects";
import { ScopeTable } from "./_components/scope-table";
import { SubjectInspection } from "./_components/subject-inspection";
import { SubjectPicker } from "./_components/subject-picker";

/** Head metadata for the attachments route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Attachments — garam" };

type Props = {
  params: Promise<{ org: string }>;
  searchParams: Promise<{ subject?: string | string[] }>;
};

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
  const [organization, policies, teams, people, agents] = await Promise.all([
    getOrg(org),
    listPolicies(org),
    lookupGroups(org),
    lookupOrgMembers(org),
    lookupAgents(org),
  ]);
  const header = (
    <>
      <PageHeader
        title="Permissions"
        description="Which policies are attached to which teams, people and agents."
      />
      <PermissionsViews org={org} current="attachments" />
    </>
  );

  if (
    organization.status === "unreachable" ||
    policies.status === "unreachable" ||
    teams.status === "unreachable" ||
    people.status === "unreachable" ||
    agents.status === "unreachable"
  ) {
    return (
      <>
        {header}
        <ApiUnreachable subject="this organization's attachments" />
      </>
    );
  }
  if (policies.status === "forbidden") {
    return (
      <>
        {header}
        <PoliciesRefused />
      </>
    );
  }

  const catalogs: Catalogs = { teams, people, agents };
  const lookup = resolveSubjectQuery(
    typeof query === "string" ? query : undefined,
    catalogs,
  );
  const inspectsMembership =
    lookup.status === "found" && lookup.subject.kind !== "team";
  const [scopeReads, membershipReads] = await Promise.all([
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
  ]);
  if (
    scopeReads.some((read) => read.status === "unreachable") ||
    membershipReads.some((read) => read.status === "unreachable")
  ) {
    return (
      <>
        {header}
        <ApiUnreachable subject="this organization's attachments" />
      </>
    );
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

  return (
    <>
      {header}
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
        />
      ) : lookup.status === "missing" ? (
        <p role="alert" className="text-sm text-destructive">
          No team, person or agent of this organization matches this address.
        </p>
      ) : lookup.status === "refused" ? (
        <p role="alert" className="text-sm text-muted-foreground">
          This organization&apos;s {KIND_PLURAL[lookup.kind]} are not listed to
          you, so this one cannot be shown.
        </p>
      ) : scopes.length === 0 ? (
        <EmptyState
          icon={ShieldCheck}
          title="No policies"
          description="This organization has no policies to attach."
        />
      ) : (
        <ScopeTable
          policyHref={policyHref}
          subjectHref={subjectHref}
          scopes={scopes}
          catalogs={catalogs}
        />
      )}
    </>
  );
}
