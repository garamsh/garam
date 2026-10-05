import type { Metadata } from "next";
import { LayoutTemplate } from "lucide-react";

import {
  getOrg,
  hostedFor,
  listAgentTemplates,
  listHostedOperators,
} from "@/features/orgs";

import { ApiUnreachable } from "../../../../_components/api-unreachable";
import { EmptyState } from "../../../../_components/empty-state";
import { PageHeader } from "../../../../_components/page-header";
import { AgentsTabs } from "../_components/agents-tabs";
import { ControlNotice } from "../_components/control-notice";
import { OperatorTemplates } from "../_components/operator-templates";
import { TemplateList } from "./_components/template-list";

/** Head metadata for the templates route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Agent templates — garam" };

type Params = { params: Promise<{ org: string }> };

/**
 * The templates the hosted operator's control service has published
 * (#1170), which agents are created from, and below them the
 * organization's garam templates, retired read-only and shown as
 * records (ADR-0088). The organization is read for itself, so one the
 * caller cannot read answers 404 (ADR-0011); inside it, a refused list
 * says so. A control service that is missing or unavailable disables
 * only its section.
 */
export default async function AgentTemplatesPage({ params }: Params) {
  const { org } = await params;
  const [organization, templates, hosted] = await Promise.all([
    getOrg(org),
    listAgentTemplates(org),
    listHostedOperators(org),
  ]);

  return (
    <>
      <PageHeader
        title="Agents"
        description="Templates are starting points for new agents. An agent copies a template once, when it is created."
      />
      <AgentsTabs org={org} current="templates" />
      {organization.status === "ok" && (
        <section className="flex flex-col gap-3">
          <h2 className="font-heading text-lg font-semibold">
            Control service templates
          </h2>
          {hosted.status === "ok" ? (
            <OperatorTemplates
              org={org}
              orgGrn={organization.data.grn}
              hosted={hostedFor(hosted.data, "agent-template:read")}
            />
          ) : (
            <ControlNotice>{hosted.reason}</ControlNotice>
          )}
        </section>
      )}
      <section className="flex flex-col gap-3">
        <h2 className="font-heading text-lg font-semibold">
          Archived garam templates
        </h2>
        <p className="text-sm text-muted-foreground">
          garam&apos;s own templates are retired. Each is kept as a record of
          what it said and cannot be changed or used to define an agent.
        </p>
        {organization.status === "unreachable" ||
        templates.status === "unreachable" ? (
          <ApiUnreachable subject="this organization's archived templates" />
        ) : templates.status === "forbidden" ? (
          <p role="alert" className="text-sm text-muted-foreground">
            garam refused to show this organization&apos;s templates to you.
          </p>
        ) : templates.data.length === 0 ? (
          <EmptyState
            icon={LayoutTemplate}
            title="No archived templates"
            description="This organization kept no templates in garam."
          />
        ) : (
          <TemplateList org={org} templates={templates.data} />
        )}
      </section>
    </>
  );
}
