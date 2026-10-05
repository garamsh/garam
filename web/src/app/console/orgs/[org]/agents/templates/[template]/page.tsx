import type { Metadata } from "next";

import { getAgentTemplate, getOrg } from "@/features/orgs";

import { ApiUnreachable } from "../../../../../_components/api-unreachable";
import { ItemCrumb } from "../../../../../_components/item-crumb";
import { PageHeader } from "../../../../../_components/page-header";
import { DesiredValues } from "../../_components/agent-facts";

/** Head metadata for a template's route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Agent template — garam" };

type Params = { params: Promise<{ org: string; template: string }> };

/**
 * One archived template: the record of what it said. garam's templates
 * are retired read-only, so the page offers no edit, no delete and no
 * agent created from it (ADR-0088). The organization is read for
 * itself, so one the caller cannot read answers 404 (ADR-0011); inside
 * it, a refused template says so.
 */
export default async function AgentTemplatePage({ params }: Params) {
  const { org, template } = await params;
  const [organization, read] = await Promise.all([
    getOrg(org),
    getAgentTemplate(org, template),
  ]);

  if (organization.status === "unreachable" || read.status === "unreachable") {
    return (
      <>
        <PageHeader title="Template" description="An archived template." />
        <ApiUnreachable subject="this template" />
      </>
    );
  }
  if (read.status === "forbidden") {
    return (
      <>
        <PageHeader title="Template" description="An archived template." />
        <p role="alert" className="text-sm text-muted-foreground">
          garam refused to show this template to you.
        </p>
      </>
    );
  }

  const { name, description, values, updatedAt } = read.data;
  return (
    <>
      <ItemCrumb label={name} />
      <PageHeader
        title={name}
        description="Archived: kept as a record of what this template said. It cannot be changed or used to define an agent."
      />
      <section className="flex flex-col gap-3">
        <dl className="grid max-w-2xl grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
          <dt className="text-muted-foreground">Identifier</dt>
          <dd className="font-mono text-xs">{template}</dd>
          <dt className="text-muted-foreground">Description</dt>
          <dd>{description === "" ? "None" : description}</dd>
          <dt className="text-muted-foreground">Last changed</dt>
          <dd>
            <time dateTime={updatedAt}>{updatedAt.slice(0, 10)}</time>
          </dd>
        </dl>
        <h2 className="font-heading text-lg font-semibold">Values</h2>
        <DesiredValues values={values} />
      </section>
    </>
  );
}
