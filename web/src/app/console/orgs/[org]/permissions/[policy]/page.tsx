import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { Banner } from "@/components/banner";
import {
  KIND_LABEL,
  getOrg,
  listPolicies,
  listPolicyAttachments,
  listPolicyVersions,
  lookupAgentDefinitions,
  lookupAgents,
  lookupGroups,
  lookupOrgMembers,
  slugFromGrn,
  type Policy,
} from "@/features/orgs";
import { checkPermissions } from "@/lib/permission-check";
import { formatPolicyDocument, hasCondition } from "@/lib/policy-document";

import { ApiUnreachable } from "../../../../_components/api-unreachable";
import { ItemCrumb } from "../../../../_components/item-crumb";
import { PageHeader } from "../../../../_components/page-header";
import { RefusedRead } from "../../../../_components/refused-read";
import { DetailPage } from "../../../../_components/templates/detail-page";
import { AttachPolicyForm } from "../_components/attach-policy-form";
import { PolicyStatus } from "../_components/policy-status";
import {
  KIND_PLURAL,
  describeSubject,
  listSubjects,
  partlyListedAgents,
  reachOf,
  reachSentence,
  refusedKinds,
  subjectChoices,
  subjectLabel,
  type AttachedSubject,
  type Catalogs,
} from "../_components/subjects";
import { Outcome, OutcomeProvider } from "../../../../_components/outcome";
import { ActivatePolicyDialog } from "./_components/activate-policy-dialog";
import { ArchivePolicyDialog } from "./_components/archive-policy-dialog";
import {
  AttachmentTable,
  type AttachmentRow,
} from "./_components/attachment-table";
import { DocumentPanel } from "./_components/document-panel";
import { VersionTable } from "./_components/version-table";

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

/** An attachment's subject as its row shows it. */
function attachmentRow(subject: AttachedSubject): AttachmentRow {
  return {
    grn: subject.grn,
    label: subject.label,
    identifier: subject.identifier,
    kind: KIND_LABEL[subject.kind],
    sentenceLabel: subjectLabel(subject),
    note:
      subject.lookup === "missing"
        ? { tone: "danger", text: "Not found in this organization" }
        : subject.lookup === "refused" && subject.kind !== "unknown"
          ? {
              tone: "muted",
              text: `Not checked: this organization's ${KIND_PLURAL[subject.kind]} are not listed to you`,
            }
          : null,
  };
}

/** The page's header where no policy could be shown. */
function Unshown({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="Policy" description="A policy of this organization." />
      {children}
    </div>
  );
}

/**
 * One policy, on the Detail template: its document and editor, its
 * versions, and whom it is attached to, with archiving in the danger
 * zone (ADR-0103).
 *
 * garam has no read of one policy, so the page finds it in the
 * organization's list, and one the list does not hold is not found;
 * its versions and attachments answer an empty list for any
 * identifier. The organization's teams, people and agents are read
 * beside it so each attachment, a bare GRN, is shown by name and type
 * (ADR-0073). The server is asked once which of the writes the page
 * offers the caller may take, and only an explicit deny withdraws one.
 */
export default async function PolicyPage({ params }: Params) {
  const { org, policy: slug } = await params;
  const resource = { type: "policy", id: slug } as const;
  const [
    organization,
    policies,
    versions,
    attachments,
    teams,
    people,
    agents,
    definitions,
    permissions,
  ] = await Promise.all([
    getOrg(org),
    listPolicies(org),
    listPolicyVersions(org, slug),
    listPolicyAttachments(org, slug),
    lookupGroups(org),
    lookupOrgMembers(org),
    lookupAgents(org),
    lookupAgentDefinitions(org),
    checkPermissions(org, {
      update: { action: "policy:update", resource },
      attach: { action: "policy:attach", resource },
      detach: { action: "policy:detach", resource },
    }),
  ]);

  if (
    organization.status === "unreachable" ||
    policies.status === "unreachable" ||
    versions.status === "unreachable" ||
    attachments.status === "unreachable" ||
    teams.status === "unreachable" ||
    people.status === "unreachable" ||
    agents.status === "unreachable" ||
    definitions.status === "unreachable"
  ) {
    return (
      <Unshown>
        <ApiUnreachable subject="this policy" />
      </Unshown>
    );
  }
  if (policies.status === "forbidden") {
    return (
      <Unshown>
        <RefusedRead subject="this organization's policies" />
      </Unshown>
    );
  }
  const policy = policies.data.find(
    (candidate) => slugFromGrn(candidate.grn) === slug,
  );
  if (policy === undefined) notFound();

  const canUpdate = permissions.get("update") !== "denied";
  const canAttach = permissions.get("attach") !== "denied";
  const canDetach = permissions.get("detach") !== "denied";
  const catalogs: Catalogs = { teams, people, agents, definitions };
  const partlyListed = partlyListedAgents(catalogs);
  const reach = reachOf(attachments, catalogs);
  const sentence = reachSentence(reach);
  const orgWide = reach.kind !== "attached";
  const active = policy.status === "active";
  const stored = versions.status === "ok" ? versions.data : null;
  const defaultDocument = stored?.find(
    (version) => version.version === policy.defaultVersion,
  )?.document;
  const defaultText =
    defaultDocument === undefined
      ? null
      : formatPolicyDocument(defaultDocument);
  const attached = new Set(
    attachments.status === "ok"
      ? attachments.data.map((attachment) => attachment.subjectGrn)
      : [],
  );
  const candidates = listSubjects(catalogs).filter(
    (subject) => !attached.has(subject.grn),
  );
  const refused = refusedKinds(catalogs);

  const overview = (
    <Banner
      tone={active && reach.kind !== "attached" ? "warn" : "info"}
      title={statusSentence(policy)}
    >
      <p>
        {active
          ? sentence
          : `Not evaluated while ${policy.status}. Once active: ${sentence}`}
      </p>
      {active && reach.kind === "org-wide" && (
        <p>
          Attaching narrows it: it then applies only to what it is attached to,
          not to everyone.
        </p>
      )}
    </Banner>
  );

  return (
    <OutcomeProvider>
      <ItemCrumb label={policy.name} />
      <DetailPage
        name={policy.name}
        idLabel="Policy identifier"
        id={slug}
        status={<PolicyStatus status={policy.status} />}
        actions={
          <>
            <Outcome area="lifecycle" />
            {!active && canUpdate && (
              <ActivatePolicyDialog
                org={org}
                policy={slug}
                policyName={policy.name}
                version={policy.defaultVersion}
                reach={sentence}
                orgWide={orgWide}
                conditioned={
                  defaultDocument !== undefined && hasCondition(defaultDocument)
                }
              />
            )}
          </>
        }
        tabs={[
          {
            value: "document",
            label: "Document",
            content: (
              <>
                {overview}
                <DocumentPanel
                  org={org}
                  policy={slug}
                  policyName={policy.name}
                  active={active}
                  defaultVersion={policy.defaultVersion}
                  nextVersion={
                    stored === null
                      ? null
                      : Math.max(
                          0,
                          ...stored.map((version) => version.version),
                        ) + 1
                  }
                  defaultText={defaultText}
                  reach={sentence}
                  orgWide={orgWide}
                  canUpdate={canUpdate}
                />
              </>
            ),
          },
          {
            value: "versions",
            label: stored === null ? "Versions" : `Versions (${stored.length})`,
            content:
              stored === null ? (
                <RefusedRead subject="this policy's versions" />
              ) : (
                <>
                  <Outcome area="versions" />
                  <VersionTable
                    rows={stored.map((version) => {
                      const author = describeSubject(
                        version.createdBy,
                        catalogs,
                      );
                      return {
                        version: version.version,
                        text: formatPolicyDocument(version.document),
                        savedAt: version.createdAt,
                        author: author.label ?? "someone not listed to you",
                        conditioned: hasCondition(version.document),
                      };
                    })}
                    context={{
                      org,
                      policy: slug,
                      defaultVersion: policy.defaultVersion,
                      defaultText,
                      active,
                      reach: sentence,
                      orgWide,
                      canUpdate,
                    }}
                  />
                </>
              ),
          },
          {
            value: "attachments",
            label:
              reach.kind === "attached"
                ? `Attachments (${reach.subjects.length})`
                : "Attachments",
            content: (
              <>
                <Outcome area="attachments" />
                {attachments.status === "forbidden" ? (
                  <RefusedRead subject="this policy's attachments">
                    It may apply to everyone in this organization.
                  </RefusedRead>
                ) : (
                  <AttachmentTable
                    org={org}
                    policy={slug}
                    policyName={policy.name}
                    subjects={
                      reach.kind === "attached"
                        ? reach.subjects.map(attachmentRow)
                        : []
                    }
                    canDetach={canDetach}
                    empty={
                      active
                        ? "It applies to everyone in this organization."
                        : "Activated like this, it would apply to everyone in this organization."
                    }
                  />
                )}
                {partlyListed.length > 0 && (
                  <p className="text-muted-foreground">
                    This organization&apos;s {partlyListed.join(" and ")} are
                    not listed to you, so some agents may be missing here.
                  </p>
                )}
                {canAttach && (
                  <section
                    aria-labelledby="attach-title"
                    className="flex flex-col gap-3 rounded-xl border bg-card p-4"
                  >
                    <h2 id="attach-title" className="text-base">
                      Attach
                    </h2>
                    {refused.length > 0 && (
                      <p className="text-muted-foreground">
                        This organization&apos;s{" "}
                        {refused.map((kind) => KIND_PLURAL[kind]).join(" and ")}{" "}
                        are not listed to you, so none of them can be chosen
                        here.
                      </p>
                    )}
                    <AttachPolicyForm
                      org={org}
                      fixed={{
                        name: "policy",
                        value: slug,
                        label: policy.name,
                      }}
                      choose={{
                        label: "Team, person or agent",
                        placeholder: "Choose what to attach to",
                        groups: subjectChoices(
                          candidates,
                          (subject) => subject.grn,
                        ),
                      }}
                      empty="Every team, person and agent you can list is already attached."
                    />
                  </section>
                )}
              </>
            ),
          },
        ]}
        dangerZone={
          canUpdate && policy.status !== "archived"
            ? [
                {
                  title: "Archive this policy",
                  description:
                    "It stops taking part in checks. Its versions and attachments are kept, and it can be activated again.",
                  control: (
                    <ArchivePolicyDialog
                      org={org}
                      policy={slug}
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
