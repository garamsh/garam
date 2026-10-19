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
} from "@/features/orgs";
import { formatPolicyDocument, hasCondition } from "@/lib/policy-document";

import { ApiUnreachable } from "../../../../_components/api-unreachable";
import { ItemCrumb } from "../../../../_components/item-crumb";
import { PageHeader } from "../../../../_components/page-header";
import { RefusedRead } from "../../../../_components/refused-read";
import { DetailPage } from "../../../../_components/templates/detail-page";
import {
  YourAccess,
  accessDecisions,
  readAccess,
} from "../../../../_components/your-access";
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
import { DeletePolicyDialog } from "./_components/delete-policy-dialog";
import {
  AttachmentTable,
  type AttachmentRow,
} from "./_components/attachment-table";
import { DocumentPanel } from "./_components/document-panel";
import { VersionTable } from "./_components/version-table";

/** Head metadata for a policy's route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Policy — garam" };

type Params = { params: Promise<{ org: string; policy: string }> };

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
        ? { tone: "danger", text: "Not found" }
        : subject.lookup === "refused" && subject.kind !== "unknown"
          ? {
              tone: "muted",
              text: "Not checked",
            }
          : null,
  };
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
 * One policy, on the Detail template: its document and editor, its
 * versions, and whom it is attached to, with deleting in the danger
 * zone (ADR-0103, ADR-0117).
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
  // Started before the page's own reads, so it runs beside them.
  const access = readAccess(org, [
    {
      key: "update",
      label: "Edit it",
      check: {
        action: "policy:update",
        resource,
      },
    },
    {
      key: "attach",
      label: "Attach it",
      check: {
        action: "policy:attach",
        resource,
      },
    },
    {
      key: "detach",
      label: "Detach it",
      check: {
        action: "policy:detach",
        resource,
      },
    },
    {
      key: "delete",
      label: "Delete it",
      check: {
        action: "policy:delete",
        resource,
      },
    },
  ]);
  const [
    organization,
    policies,
    versions,
    attachments,
    teams,
    people,
    agents,
    definitions,
    readings,
  ] = await Promise.all([
    getOrg(org),
    listPolicies(org),
    listPolicyVersions(org, slug),
    listPolicyAttachments(org, slug),
    lookupGroups(org),
    lookupOrgMembers(org),
    lookupAgents(org),
    lookupAgentDefinitions(org),
    access,
  ]);
  const permissions = accessDecisions(readings);

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
  const canDelete = permissions.get("delete") !== "denied";
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

  // The header shows the status; the banner says whom it reaches.
  const overview = (
    <Banner
      tone={active && reach.kind !== "attached" ? "warn" : "info"}
      title={active ? sentence : `Once active: ${sentence}`}
    />
  );

  return (
    <OutcomeProvider>
      <ItemCrumb label={policy.name} />
      <DetailPage
        name={policy.name}
        idLabel="Identifier"
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
                    It may apply to everyone.
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
                        ? "Applies to everyone."
                        : "Would apply to everyone once active."
                    }
                  />
                )}
                {partlyListed.length > 0 && (
                  <p className="text-muted-foreground">
                    Some agents may be missing: {partlyListed.join(" and ")} not
                    listed to you.
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
                        Not listed to you:{" "}
                        {refused.map((kind) => KIND_PLURAL[kind]).join(" and ")}
                        .
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
                        label: "Attach to",
                        placeholder: "Choose",
                        groups: subjectChoices(
                          candidates,
                          (subject) => subject.grn,
                        ),
                      }}
                      empty="Nothing left to attach."
                    />
                  </section>
                )}
              </>
            ),
          },
        ]}
        dangerZone={
          canDelete
            ? [
                {
                  title: "Delete this policy",
                  control: (
                    <DeletePolicyDialog
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
      <YourAccess readings={readings} />
    </OutcomeProvider>
  );
}
