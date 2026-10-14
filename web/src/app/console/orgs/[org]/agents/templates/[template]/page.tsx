import type { Metadata } from "next";

import { Banner } from "@/components/banner";
import { DescriptionList } from "@/components/description-list";
import { StatusDot } from "@/components/status-dot";
import { Time } from "@/components/time";
import { getAgentTemplate, getOrg } from "@/features/orgs";

import { ApiUnreachable } from "../../../../../_components/api-unreachable";
import { ItemCrumb } from "../../../../../_components/item-crumb";
import { PageHeader } from "../../../../../_components/page-header";
import { DetailPage } from "../../../../../_components/templates/detail-page";
import { DesiredValues } from "../../_components/agent-facts";

/** Head metadata for a template's route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Agent template — garam" };

type Params = { params: Promise<{ org: string; template: string }> };

/**
 * One archived template: the record of what it said. garam's templates
 * are retired read-only, so the page offers no edit, no delete and no
 * agent created from it (ADR-0088), and has no Settings or Danger zone
 * to hold one. The organization is read for itself, so one the caller
 * cannot read answers 404 (ADR-0011); inside it, a refused template
 * says so.
 */
export default async function AgentTemplatePage({ params }: Params) {
  const { org, template } = await params;
  const [organization, read] = await Promise.all([
    getOrg(org),
    getAgentTemplate(org, template),
  ]);

  const fallback = (
    <PageHeader title="Template" description="An archived template." />
  );
  if (organization.status === "unreachable" || read.status === "unreachable") {
    return (
      <>
        {fallback}
        <ApiUnreachable subject="this template" />
      </>
    );
  }
  if (read.status === "forbidden") {
    return (
      <>
        {fallback}
        <Banner tone="warn" title="This template is not shown to you">
          garam refused to show this template to you. An owner or admin of the
          organization can grant it.
        </Banner>
      </>
    );
  }

  const { name, description, values, updatedAt } = read.data;
  return (
    <>
      <ItemCrumb label={name} />
      <DetailPage
        name={name}
        idLabel="Template identifier"
        id={template}
        status={<StatusDot tone="neutral" label="Archived" />}
        tabs={[
          {
            value: "overview",
            label: "Overview",
            content: (
              <>
                <Banner tone="info" title="Kept as a record">
                  garam&apos;s templates are retired. This one cannot be changed
                  or used to define an agent.
                </Banner>
                <DescriptionList
                  items={[
                    {
                      term: "Description",
                      description:
                        description === "" ? (
                          <span className="text-muted-foreground">None</span>
                        ) : (
                          description
                        ),
                    },
                    {
                      term: "Last changed",
                      description: <Time value={updatedAt} />,
                    },
                  ]}
                />
              </>
            ),
          },
          {
            value: "values",
            label: "Values",
            content: <DesiredValues values={values} />,
          },
        ]}
      />
    </>
  );
}
