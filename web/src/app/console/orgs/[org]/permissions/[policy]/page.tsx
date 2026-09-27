import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { Separator } from "@/components/ui/separator";
import {
  getOrg,
  listPolicies,
  listPolicyAttachments,
  listPolicyVersions,
  lookupAgents,
  lookupGroups,
  lookupOrgMembers,
  slugFromGrn,
  type Policy,
} from "@/features/orgs";
import { formatPolicyDocument } from "@/lib/policy-document";

import { ApiUnreachable } from "../../../../_components/api-unreachable";
import { ItemCrumb } from "../../../../_components/item-crumb";
import { PageHeader } from "../../../../_components/page-header";
import { AttachPolicyForm } from "../_components/attach-policy-form";
import { PoliciesRefused } from "../_components/policies-refused";
import { PolicyStatusBadge } from "../_components/policy-status-badge";
import {
  KIND_PLURAL,
  describeSubject,
  listSubjects,
  reachOf,
  reachSentence,
  refusedKinds,
  subjectChoices,
  type Catalogs,
} from "../_components/subjects";
import { ActivatePolicyForm } from "./_components/activate-policy-form";
import { AddVersionForm } from "./_components/add-version-form";
import { ArchivePolicyForm } from "./_components/archive-policy-form";
import { AttachmentTable } from "./_components/attachment-table";
import { VersionList } from "./_components/version-list";

/** Head metadata for a policy's route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Policy — garam" };

type Params = { params: Promise<{ org: string; policy: string }> };

/** What the policy's state means for checks, in one sentence. */
function statusSentence(policy: Policy): string {
  switch (policy.status) {
    case "draft":
      return "Draft: grants no access until it is activated.";
    case "active":
      return `Active: version ${policy.defaultVersion} takes part in every check it reaches.`;
    case "archived":
      return "Archived: grants nothing. Its versions and attachments are kept.";
  }
}

/**
 * One policy: its state, its versions and whom it is attached to.
 *
 * garam has no read of one policy, so the page finds it in the
 * organization's list, and one the list does not hold is not found;
 * its versions and attachments answer an empty list for any
 * identifier. The organization's teams, people and agents are read
 * beside it so each attachment, a bare GRN, is shown by name and type
 * (ADR-0073).
 */
export default async function PolicyPage({ params }: Params) {
  const { org, policy: slug } = await params;
  const [organization, policies, versions, attachments, teams, people, agents] =
    await Promise.all([
      getOrg(org),
      listPolicies(org),
      listPolicyVersions(org, slug),
      listPolicyAttachments(org, slug),
      lookupGroups(org),
      lookupOrgMembers(org),
      lookupAgents(org),
    ]);

  if (
    organization.status === "unreachable" ||
    policies.status === "unreachable" ||
    versions.status === "unreachable" ||
    attachments.status === "unreachable" ||
    teams.status === "unreachable" ||
    people.status === "unreachable" ||
    agents.status === "unreachable"
  ) {
    return (
      <>
        <PageHeader
          title="Policy"
          description="A policy of this organization."
        />
        <ApiUnreachable subject="this policy" />
      </>
    );
  }
  if (policies.status === "forbidden") {
    return (
      <>
        <PageHeader
          title="Policy"
          description="A policy of this organization."
        />
        <PoliciesRefused />
      </>
    );
  }
  const policy = policies.data.find(
    (candidate) => slugFromGrn(candidate.grn) === slug,
  );
  if (policy === undefined) notFound();

  const catalogs: Catalogs = { teams, people, agents };
  const reach = reachOf(attachments, catalogs);
  const sentence = reachSentence(reach);
  const orgWide = reach.kind !== "attached";
  const active = policy.status === "active";
  const defaultDocument =
    versions.status === "ok"
      ? versions.data.find(
          (version) => version.version === policy.defaultVersion,
        )?.document
      : undefined;
  const attached = new Set(
    attachments.status === "ok"
      ? attachments.data.map((attachment) => attachment.subjectGrn)
      : [],
  );
  const candidates = listSubjects(catalogs).filter(
    (subject) => !attached.has(subject.grn),
  );
  const refused = refusedKinds(catalogs);

  return (
    <>
      <ItemCrumb label={policy.name} />
      <PageHeader title={policy.name} description={statusSentence(policy)}>
        <PolicyStatusBadge status={policy.status} />
      </PageHeader>

      <section className="flex flex-col gap-3">
        <h2 className="font-heading text-lg font-semibold">Scope</h2>
        <p className="text-sm">
          {active
            ? sentence
            : `Not evaluated while ${policy.status}. Once active: ${sentence}`}
        </p>
        {policy.status === "active" ? (
          <ArchivePolicyForm org={org} policy={slug} policyName={policy.name} />
        ) : (
          <div className="flex flex-wrap gap-3">
            <ActivatePolicyForm
              org={org}
              policy={slug}
              policyName={policy.name}
              version={policy.defaultVersion}
              reach={sentence}
              orgWide={orgWide}
            />
            {policy.status === "draft" && (
              <ArchivePolicyForm
                org={org}
                policy={slug}
                policyName={policy.name}
              />
            )}
          </div>
        )}
      </section>

      <Separator />
      <section className="flex flex-col gap-3">
        <h2 className="font-heading text-lg font-semibold">Attachments</h2>
        {attachments.status === "forbidden" ? (
          <p className="text-sm text-muted-foreground">
            garam refused to list this policy&apos;s attachments to you.
          </p>
        ) : reach.kind === "attached" ? (
          <AttachmentTable
            org={org}
            policy={slug}
            policyName={policy.name}
            subjects={reach.subjects}
          />
        ) : (
          <p className="text-sm text-muted-foreground">
            No attachments.{" "}
            {active
              ? "It applies to everyone in this organization."
              : "Activated like this, it would apply to everyone in this organization."}
          </p>
        )}
        {active && reach.kind === "org-wide" && (
          <p className="text-sm text-muted-foreground">
            Attaching narrows it: it then applies only to what it is attached
            to, not to everyone.
          </p>
        )}
        {refused.length > 0 && (
          <p className="text-sm text-muted-foreground">
            This organization&apos;s{" "}
            {refused.map((kind) => KIND_PLURAL[kind]).join(" and ")} are not
            listed to you, so none of them can be chosen here.
          </p>
        )}
        <AttachPolicyForm
          org={org}
          fixed={{ name: "policy", value: slug }}
          choose={{
            label: "Team, person or agent",
            placeholder: "Choose what to attach to",
            groups: subjectChoices(candidates, (subject) => subject.grn),
          }}
          empty="Every team, person and agent you can list is already attached."
        />
      </section>

      <Separator />
      <section className="flex flex-col gap-3">
        <h2 className="font-heading text-lg font-semibold">Versions</h2>
        <p className="text-sm text-muted-foreground">
          Version {policy.defaultVersion} is the default. A saved version
          changes nothing until it is made the default.
        </p>
        {versions.status === "forbidden" ? (
          <p className="text-sm text-muted-foreground">
            garam refused to list this policy&apos;s versions to you.
          </p>
        ) : (
          <VersionList
            org={org}
            policy={slug}
            versions={versions.data}
            defaultVersion={policy.defaultVersion}
            authorOf={(version) => {
              const author = describeSubject(version.createdBy, catalogs);
              return author.label ?? author.identifier;
            }}
            active={active}
            reach={sentence}
            orgWide={orgWide}
          />
        )}
        <AddVersionForm
          org={org}
          policy={slug}
          defaultVersion={policy.defaultVersion}
          initialDocument={formatPolicyDocument(
            defaultDocument ?? { Version: "2026-07-25", Statement: [] },
          )}
        />
      </section>
    </>
  );
}
