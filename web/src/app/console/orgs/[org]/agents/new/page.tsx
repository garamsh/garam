import type { Metadata } from "next";

import {
  getOrg,
  hostedFor,
  listHostedOperators,
  lookupOperators,
  slugFromGrn,
} from "@/features/orgs";

import { ApiUnreachable } from "../../../../_components/api-unreachable";
import { PageHeader } from "../../../../_components/page-header";
import { ControlNotice } from "../_components/control-notice";
import { CreateFromTemplate } from "../_components/create-from-template";
import { DefineAgentForm } from "../_components/define-agent-form";

/** Head metadata for the create-agent route; Next.js reads it by name. */
export const metadata: Metadata = { title: "New agent — garam" };

type Params = { params: Promise<{ org: string }> };

/**
 * Creates an agent from a template the hosted operator's control
 * service published (#1170), or defines one in garam from a blank
 * form. garam's own templates are retired read-only and offer no start
 * here (ADR-0088). A control service that is missing or unavailable
 * disables only its section.
 */
export default async function NewAgentPage({ params }: Params) {
  const { org } = await params;
  const [operators, organization, hosted] = await Promise.all([
    lookupOperators(org),
    getOrg(org),
    listHostedOperators(org),
  ]);

  if (operators.status === "unreachable") {
    return (
      <>
        <PageHeader
          title="New agent"
          description="Create from a template or define one blank."
        />
        <ApiUnreachable subject="what a new agent starts from" />
      </>
    );
  }

  return (
    <>
      <PageHeader
        title="New agent"
        description="Create from a template or define one blank, then adjust before creating."
      />
      {organization.status === "ok" && (
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
      )}
      <h2 className="font-heading text-lg font-semibold">Define in garam</h2>
      <DefineAgentForm
        org={org}
        operators={
          operators.status === "ok"
            ? operators.data.map((operator) => ({
                identifier: slugFromGrn(operator.grn),
                name: operator.name,
              }))
            : null
        }
        values={{}}
      />
    </>
  );
}
