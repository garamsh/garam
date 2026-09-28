import type { Metadata } from "next";
import Link from "next/link";
import { LayoutTemplate } from "lucide-react";

import { Button } from "@/components/ui/button";
import { getOrg, listAgentTemplates } from "@/features/orgs";

import { ApiUnreachable } from "../../../../_components/api-unreachable";
import { EmptyState } from "../../../../_components/empty-state";
import { PageHeader } from "../../../../_components/page-header";
import { AgentsTabs } from "../_components/agents-tabs";
import { TemplateList } from "./_components/template-list";

/** Head metadata for the templates route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Agent templates — garam" };

type Params = { params: Promise<{ org: string }> };

/**
 * The organization's agent templates, inside Agents (ADR-0075). The
 * organization is read for itself, so one the caller cannot read
 * answers 404 (ADR-0011); inside it, a refused list says so.
 */
export default async function AgentTemplatesPage({ params }: Params) {
  const { org } = await params;
  const [organization, templates] = await Promise.all([
    getOrg(org),
    listAgentTemplates(org),
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
    </>
  );
}
