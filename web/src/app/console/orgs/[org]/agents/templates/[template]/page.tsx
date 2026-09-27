import type { Metadata } from "next";
import Link from "next/link";

import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { getAgentTemplate, getOrg } from "@/features/orgs";

import { ApiUnreachable } from "../../../../../_components/api-unreachable";
import { ItemCrumb } from "../../../../../_components/item-crumb";
import { PageHeader } from "../../../../../_components/page-header";
import { TemplateForm } from "../_components/template-form";
import { DeleteTemplateForm } from "./_components/delete-template-form";

/** Head metadata for a template's route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Agent template — garam" };

type Params = { params: Promise<{ org: string; template: string }> };

/**
 * One template: what it says, the edit that replaces it, and deleting
 * it. Neither reaches an agent already created from it (ADR-0075). The
 * organization is read for itself, so one the caller cannot read
 * answers 404 (ADR-0011); inside it, a refused template says so.
 */
export default async function AgentTemplatePage({ params }: Params) {
  const { org, template } = await params;
  const [organization, read] = await Promise.all([
    getOrg(org),
    getAgentTemplate(org, template),
  ]);
  const listHref = `/console/orgs/${org}/agents/templates`;

  if (organization.status === "unreachable" || read.status === "unreachable") {
    return (
      <>
        <PageHeader title="Template" description="An agent template." />
        <ApiUnreachable subject="this template" />
      </>
    );
  }
  if (read.status === "forbidden") {
    return (
      <>
        <PageHeader title="Template" description="An agent template." />
        <p role="alert" className="text-sm text-muted-foreground">
          garam refused to show this template to you.
        </p>
      </>
    );
  }

  const { name, description, values } = read.data;
  return (
    <>
      <ItemCrumb label={name} />
      <PageHeader
        title={name}
        description="Edits apply to agents created after them, not to agents already created."
      >
        <Button asChild>
          <Link
            href={`/console/orgs/${org}/agents/new?template=${encodeURIComponent(template)}`}
          >
            Create agent from template
          </Link>
        </Button>
      </PageHeader>
      <TemplateForm
        org={org}
        cancelHref={listHref}
        start={{ mode: "edit", template, name, description, values }}
      />
      <Separator />
      <DeleteTemplateForm org={org} template={template} name={name} />
    </>
  );
}
