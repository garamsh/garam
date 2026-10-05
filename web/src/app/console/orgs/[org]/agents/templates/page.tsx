import type { Metadata } from "next";
import Link from "next/link";
import { LayoutTemplate } from "lucide-react";

import { Button } from "@/components/ui/button";
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
 * The organization's agent templates, inside Agents (ADR-0075), and
 * beside them the ones its hosted operator's control service has
 * published (#1170), which #1188 will leave as the only ones. The
 * organization is read for itself, so one the caller cannot read
 * answers 404 (ADR-0011); inside it, a refused list says so. A control
 * service that is missing or unavailable disables only its section.
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
      >
        <Button asChild>
          <Link href={`/console/orgs/${org}/agents/templates/new`}>
            New template
          </Link>
        </Button>
      </PageHeader>
      <AgentsTabs org={org} current="templates" />
      {organization.status === "unreachable" ||
      templates.status === "unreachable" ? (
        <ApiUnreachable subject="this organization's agent templates" />
      ) : templates.status === "forbidden" ? (
        <p role="alert" className="text-sm text-muted-foreground">
          garam refused to show this organization&apos;s templates to you.
        </p>
      ) : templates.data.length === 0 ? (
        <EmptyState
          icon={LayoutTemplate}
          title="No templates"
          description="This organization keeps no templates."
        />
      ) : (
        <TemplateList org={org} templates={templates.data} />
      )}
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
    </>
  );
}
