import type { Metadata } from "next";

import { getOrg, hostedFor, listHostedOperators } from "@/features/orgs";
import { checkPermissions } from "@/lib/permission-check";
import { REFUSAL_COPY } from "@/lib/refusal-copy";

import { ApiUnreachable } from "../../../../_components/api-unreachable";
import { PageHeader } from "../../../../_components/page-header";
import {
  NO_FIX,
  controlReach,
  type ControlReach,
} from "../_components/control-notice";
import { NewAgentWizard } from "./_components/new-agent-wizard";

/** Head metadata for the create-agent route; Next.js reads it by name. */
export const metadata: Metadata = { title: "New agent — garam" };

type Params = { params: Promise<{ org: string }> };

const TITLE = "New agent";
const DESCRIPTION =
  "Create an agent from a template the control service published.";

/**
 * Creates an agent from a template the hosted operator's control
 * service published (#1170). garam accepts no definition of its own
 * (ADR-0090) and its templates are retired read-only (ADR-0088), so
 * the control service is the one start here. Whether one can create
 * is read here, so the wizard opens on the step that is next: one that
 * cannot says why and where that is fixed. Creating is asked of the
 * server as well, and refused here only where it says no (ADR-0103).
 */
export default async function NewAgentPage({ params }: Params) {
  const { org } = await params;
  const [organization, hosted, permissions] = await Promise.all([
    getOrg(org),
    listHostedOperators(org),
    checkPermissions(org, { create: { action: "agent:create" } }),
  ]);

  if (organization.status !== "ok") {
    return (
      <>
        <PageHeader title={TITLE} description={DESCRIPTION} />
        <ApiUnreachable subject="what a new agent starts from" />
      </>
    );
  }

  const start: ControlReach =
    permissions.get("create") === "denied"
      ? { status: "blocked", block: { reason: REFUSAL_COPY.permissionDenied, fix: NO_FIX } }
      : hosted.status !== "ok"
        ? { status: "blocked", block: { reason: hosted.reason, fix: NO_FIX } }
        : controlReach(
            hostedFor(hosted.data, "agent:create"),
            "agent:create",
            "agent-template:read",
          );

  return (
    <NewAgentWizard
      org={org}
      orgGrn={organization.data.grn}
      start={start}
      controllers={
        hosted.status === "ok"
          ? Object.fromEntries(hosted.operators.map((o) => [o.grn, o.name]))
          : {}
      }
    />
  );
}
