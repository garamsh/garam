import type { Metadata } from "next";

import { PageHeader } from "../../../../../_components/page-header";
import { TemplateForm } from "../_components/template-form";

/** Head metadata for the create-template route; Next.js reads it by name. */
export const metadata: Metadata = { title: "New agent template — garam" };

type Params = { params: Promise<{ org: string }> };

/** Creates a template and opens it. */
export default async function NewAgentTemplatePage({ params }: Params) {
  const { org } = await params;

  return (
    <>
      <PageHeader
        title="New template"
        description="A starting point for new agents. It names no operator; each agent chooses its own."
      />
      <TemplateForm
        org={org}
        cancelHref={`/console/orgs/${org}/agents/templates`}
        start={{ mode: "create" }}
      />
    </>
  );
}
