import type { Metadata } from "next";

import {
  NO_FIX,
  controlReach,
  type ControlReach,
} from "@/components/control-notice";
import {
  getOrg,
  hostedFor,
  listHostedOperators,
  type HostedOperator,
} from "@/features/orgs";
import { checkPermissions } from "@/lib/permission-check";
import { REFUSAL_COPY } from "@/lib/refusal-copy";

import { ApiUnreachable } from "../../../../_components/api-unreachable";
import { PageHeader } from "../../../../_components/page-header";
import { Section } from "../../../../_components/section";
import { OperatorTemplates } from "../_components/operator-templates";
import { NewAgentWizard } from "./_components/new-agent-wizard";

/** Head metadata for the create-agent route; Next.js reads it by name. */
export const metadata: Metadata = { title: "New agent — garam" };

type Params = { params: Promise<{ org: string }> };

const TITLE = "New agent";

/**
 * Creates an agent from a template the hosted operator's control
 * service published (#1170), and below it lists and publishes those
 * templates. garam accepts no definition of its own (ADR-0090), so the
 * control service is the one start here. Whether one can create is
 * read here, so the wizard opens on the step that is next: one that
 * cannot says why and where that is fixed. Creating and publishing are
 * asked of the server as well, and refused here only where it says no.
 */
export default async function NewAgentPage({ params }: Params) {
  const { org } = await params;
  const [organization, hosted, permissions] = await Promise.all([
    getOrg(org),
    listHostedOperators(org),
    checkPermissions(org, {
      create: { action: "agent:create" },
      publish: { action: "agent-template:publish" },
    }),
  ]);

  if (organization.status !== "ok") {
    return (
      <>
        <PageHeader title={TITLE} />
        <ApiUnreachable subject="what a new agent starts from" />
      </>
    );
  }

  const start: ControlReach<HostedOperator> =
    permissions.get("create") === "denied"
      ? {
          status: "blocked",
          block: { reason: REFUSAL_COPY.permissionDenied, fix: NO_FIX },
        }
      : hosted.status !== "ok"
        ? { status: "blocked", block: { reason: hosted.reason, fix: NO_FIX } }
        : controlReach(
            hostedFor(hosted.data, "agent:create"),
            "agent:create",
            "agent-template:read",
          );

  // The wizard already says why no control service is reachable, so
  // the templates show only where one can be read.
  const templates =
    hosted.status === "ok"
      ? hostedFor(hosted.data, "agent-template:read")
      : null;

  return (
    <div className="flex flex-col gap-10">
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
      {templates !== null && (
        <Section region title="Templates">
          <OperatorTemplates
            org={org}
            orgGrn={organization.data.grn}
            hosted={templates}
            publishDenied={permissions.get("publish") === "denied"}
          />
        </Section>
      )}
    </div>
  );
}
