import type { Metadata } from "next";
import { LayoutTemplate } from "lucide-react";

import { ControlNotice, NO_FIX } from "@/components/control-notice";
import {
  getOrg,
  hostedFor,
  listAgentTemplates,
  listHostedOperators,
  slugFromGrn,
} from "@/features/orgs";
import { checkPermissions } from "@/lib/permission-check";

import { ApiUnreachable } from "../../../../_components/api-unreachable";
import { EmptyState } from "../../../../_components/empty-state";
import { orgHref } from "../../../../_components/navigation";
import { RefusedRead } from "../../../../_components/refused-read";
import { Section } from "../../../../_components/section";
import { ListPage } from "../../../../_components/templates/list-page";
import { OperatorTemplates } from "../_components/operator-templates";
import { TemplateTable } from "./_components/template-table";

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
 * only its section. Publishing is asked of the server and left out
 * only where it says no (ADR-0103).
 *
 * The header carries no count: the two lists are counted where each
 * stands, and the control service's is read in the browser.
 */
export default async function AgentTemplatesPage({ params }: Params) {
  const { org } = await params;
  const [organization, templates, hosted, permissions] = await Promise.all([
    getOrg(org),
    listAgentTemplates(org),
    listHostedOperators(org),
    checkPermissions(org, {
      publish: { action: "agent-template:publish" },
    }),
  ]);

  return (
    <ListPage
      title="Templates"
      description="Starting points for new agents. An agent copies a template once, when it is created."
    >
      {organization.status === "ok" && (
        <Section region title="Control service templates">
          {hosted.status === "ok" ? (
            <OperatorTemplates
              org={org}
              orgGrn={organization.data.grn}
              hosted={hostedFor(hosted.data, "agent-template:read")}
              publishDenied={permissions.get("publish") === "denied"}
            />
          ) : (
            <ControlNotice
              operatorsHref={orgHref(org, "operators")}
              reason={hosted.reason}
              fix={NO_FIX}
            />
          )}
        </Section>
      )}
      <Section
        region
        title="Archived garam templates"
        count={templates.status === "ok" ? templates.data.length : undefined}
      >
        <p className="text-muted-foreground">
          garam&apos;s own templates are retired. Each is kept as a record of
          what it said and cannot be changed or used to define an agent.
        </p>
        {organization.status === "unreachable" ||
        templates.status === "unreachable" ? (
          <ApiUnreachable subject="this organization's archived templates" />
        ) : templates.status === "forbidden" ? (
          <RefusedRead subject="this organization's archived templates" />
        ) : (
          <TemplateTable
            org={org}
            rows={templates.data.map((template) => ({
              identifier: slugFromGrn(template.grn),
              name: template.name,
              description: template.description,
              valueCount: Object.keys(template.values).length,
              updatedAt: template.updatedAt,
            }))}
            empty={
              <EmptyState
                icon={LayoutTemplate}
                title="No archived templates"
                description="This organization kept no templates in garam."
              />
            }
          />
        )}
      </Section>
    </ListPage>
  );
}
