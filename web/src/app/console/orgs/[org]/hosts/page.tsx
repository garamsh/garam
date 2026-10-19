import type { Metadata } from "next";

import { Button } from "@/components/ui/button";
import {
  UNNAMED_AGENT,
  getOrg,
  lookupAgentDefinitions,
  lookupAgentEnrollments,
  lookupAgentSupervisors,
  slugFromGrn,
  type AgentEnrollmentListing,
  type AgentSupervisor,
} from "@/features/orgs";
import { checkPermissions } from "@/lib/permission-check";

import { ApiUnreachable } from "../../../_components/api-unreachable";
import { ApproveHostDialog } from "../../../_components/approve-host-dialog";
import { InactiveButton } from "../../../_components/inactive-button";
import { orgHref } from "../../../_components/navigation";
import { RefusedRead } from "../../../_components/refused-read";
import { ListPage } from "../../../_components/templates/list-page";
import {
  HostLists,
  type ApprovalRow,
  type HostAgent,
  type SupervisorRow,
} from "./_components/host-lists";

/** Head metadata for the hosts route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Hosts — garam" };

type Params = { params: Promise<{ org: string }> };

const TITLE = "Hosts";
const DESCRIPTION =
  "Standalone and direct hosts: each runs one agent under a supervisor this organization approved.";

const DENIED =
  "No policy lets you manage hosts.";

const PLACEMENT = {
  standalone: "Runs its agent in containers it starts",
  direct: "Runs its agent as a runtime already running",
};

const APPROVAL_STATE: Record<
  AgentEnrollmentListing["state"],
  ApprovalRow["state"]
> = {
  pending: { tone: "info", label: "Waiting for the host" },
  spent: { tone: "ok", label: "Enrolled" },
  withdrawn: { tone: "neutral", label: "Withdrawn" },
  expired: { tone: "neutral", label: "Expired" },
};

const SUPERVISOR_STATE: Record<
  AgentSupervisor["state"],
  SupervisorRow["state"]
> = {
  active: { tone: "ok", label: "Active" },
  revoked: { tone: "neutral", label: "Revoked" },
};

/** When an approval stopped, or stops, being spendable. */
function approvalWhen(listing: AgentEnrollmentListing): ApprovalRow["when"] {
  if (listing.spentAt) return { label: "Enrolled", at: listing.spentAt };
  if (listing.withdrawnAt) {
    return { label: "Withdrawn", at: listing.withdrawnAt };
  }
  return {
    label: listing.state === "expired" ? "Expired" : "Expires",
    at: listing.expiresAt,
  };
}

/**
 * The organization's standalone and direct hosts (ADR-0082, ADR-0111):
 * its supervisors, each with the agent it runs and at which epoch, and
 * the approvals that stood them up. A host is approved from what
 * `garam standalone prepare` printed on it; an unspent approval is
 * withdrawn and an active supervisor revoked, each after a dialog
 * saying what ends. Where the server denies `agent:enroll`, each of the
 * three stays on the page, inactive, with the reason (ADR-0113). A list
 * garam refused is said, never shown as empty.
 */
export default async function HostsPage({ params }: Params) {
  const { org } = await params;
  const [organization, enrollments, supervisors, definitions, may] =
    await Promise.all([
      getOrg(org),
      lookupAgentEnrollments(org),
      lookupAgentSupervisors(org),
      lookupAgentDefinitions(org),
      checkPermissions(org, { enroll: { action: "agent:enroll" } }),
    ]);

  const denied = may.get("enroll") === "denied" ? DENIED : null;
  const approve =
    denied !== null ? (
      <InactiveButton variant="default" reason={denied}>
        Approve a host
      </InactiveButton>
    ) : (
      <ApproveHostDialog
        org={org}
        trigger={<Button type="button">Approve a host</Button>}
      />
    );

  if (
    organization.status === "unreachable" ||
    enrollments.status === "unreachable" ||
    supervisors.status === "unreachable"
  ) {
    return (
      <ListPage title={TITLE} description={DESCRIPTION} primaryAction={approve}>
        <ApiUnreachable subject="this organization's hosts" />
      </ListPage>
    );
  }

  const names = new Map(
    (definitions.status === "ok" ? definitions.data : []).map((definition) => [
      definition.agentGrn,
      definition.name,
    ]),
  );
  const agentOf = (grn: string): HostAgent => ({
    name: names.get(grn) ?? UNNAMED_AGENT,
    href: orgHref(org, `agents/${slugFromGrn(grn)}`),
  });
  const hostOf = new Map(
    (supervisors.status === "ok" ? supervisors.data : []).map((supervisor) => [
      supervisor.grn,
      supervisor.hostName,
    ]),
  );

  const approvals: ApprovalRow[] | null =
    enrollments.status === "ok"
      ? enrollments.data.map((listing) => ({
          id: listing.id,
          hostName: listing.hostName,
          state: APPROVAL_STATE[listing.state],
          placement: PLACEMENT[listing.placement],
          agent: agentOf(listing.agentGrn),
          replaces: listing.replaces
            ? `Replaces ${hostOf.get(listing.replaces.assigneeGrn) ?? "a host"} at epoch ${listing.replaces.epoch}`
            : null,
          when: approvalWhen(listing),
          withdrawable: listing.state === "pending",
        }))
      : null;
  const supervisorRows: SupervisorRow[] | null =
    supervisors.status === "ok"
      ? supervisors.data.map((supervisor) => ({
          identifier: slugFromGrn(supervisor.grn),
          hostName: supervisor.hostName,
          state: SUPERVISOR_STATE[supervisor.state],
          placement: PLACEMENT[supervisor.placement],
          agent: agentOf(supervisor.agentGrn),
          epoch: supervisor.epoch,
          lastActivatedAt: supervisor.lastActivatedAt ?? null,
          revocable: supervisor.state === "active",
        }))
      : null;

  return (
    <ListPage title={TITLE} description={DESCRIPTION} primaryAction={approve}>
      {supervisors.status === "forbidden" && (
        <RefusedRead subject="this organization's supervisors" />
      )}
      {enrollments.status === "forbidden" && (
        <RefusedRead subject="this organization's approvals" />
      )}
      <HostLists
        org={org}
        approvals={approvals}
        supervisors={supervisorRows}
        denied={denied}
      />
    </ListPage>
  );
}
