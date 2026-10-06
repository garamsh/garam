import type { Metadata } from "next";

import { getOrg, hostedFor, listHostedOperators } from "@/features/orgs";

import { ApiUnreachable } from "../../../../_components/api-unreachable";
import { PageHeader } from "../../../../_components/page-header";
import { ControlNotice } from "../_components/control-notice";
import { CreateFromTemplate } from "../_components/create-from-template";

/** Head metadata for the create-agent route; Next.js reads it by name. */
export const metadata: Metadata = { title: "New agent — garam" };

type Params = { params: Promise<{ org: string }> };

/**
 * Creates an agent from a template the hosted operator's control
 * service published (#1170). garam accepts no definition of its own
 * (ADR-0090) and its templates are retired read-only (ADR-0088), so
 * the control service is the one start here; one that is missing or
 * unavailable disables its section.
 */
export default async function NewAgentPage({ params }: Params) {
  const { org } = await params;
  const [organization, hosted] = await Promise.all([
    getOrg(org),
    listHostedOperators(org),
  ]);

  return (
    <>
      <PageHeader
        title="New agent"
        description="Create from a template the control service published."
      />
      {organization.status === "ok" ? (
        <section className="flex flex-col gap-3">
          <h2 className="font-heading text-lg font-semibold">
            From the control service
          </h2>
          {hosted.status === "ok" ? (
            <CreateFromTemplate
              org={org}
              orgGrn={organization.data.grn}
              hosted={hostedFor(hosted.data, "agent:create")}
              controllers={Object.fromEntries(
                hosted.operators.map((o) => [o.grn, o.name]),
              )}
            />
          ) : (
            <ControlNotice>{hosted.reason}</ControlNotice>
          )}
        </section>
      ) : (
        <ApiUnreachable subject="what a new agent starts from" />
      )}
    </>
  );
}
