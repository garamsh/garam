import type { Metadata } from "next";
import Link from "next/link";

import {
  getAgentTemplate,
  getOrg,
  hostedFor,
  listAgentTemplates,
  listHostedOperators,
  lookupOperators,
  slugFromGrn,
  type AgentTemplate,
} from "@/features/orgs";
import { cn } from "@/lib/utils";

import { ApiUnreachable } from "../../../../_components/api-unreachable";
import { PageHeader } from "../../../../_components/page-header";
import { CreateFromTemplate } from "../_components/create-from-template";
import { DefineAgentForm } from "../_components/define-agent-form";

/** Head metadata for the create-agent route; Next.js reads it by name. */
export const metadata: Metadata = { title: "New agent — garam" };

type Params = {
  params: Promise<{ org: string }>;
  searchParams: Promise<{ template?: string | string[] }>;
};

/**
 * Creates an agent from a template the hosted operator's control
 * service published (#1170), or defines one from a blank form or from
 * one of the organization's templates. Choosing a template copies its
 * values once; later edits to the template do not reach the agent
 * (ADR-0075). A control service that is missing or unavailable
 * disables only its section.
 */
export default async function NewAgentPage({ params, searchParams }: Params) {
  const { org } = await params;
  const { template: chosen } = await searchParams;
  const template = typeof chosen === "string" && chosen !== "" ? chosen : null;
  const [operators, templates, source, organization, hosted] =
    await Promise.all([
      lookupOperators(org),
      listAgentTemplates(org),
      template === null ? null : getAgentTemplate(org, template),
      getOrg(org),
      listHostedOperators(org),
    ]);
  const base = `/console/orgs/${org}/agents/new`;

  if (
    operators.status === "unreachable" ||
    templates.status === "unreachable" ||
    source?.status === "unreachable"
  ) {
    return (
      <>
        <PageHeader
          title="New agent"
          description="Start blank or from a template."
        />
        <ApiUnreachable subject="what a new agent starts from" />
      </>
    );
  }

  const start: AgentTemplate | null =
    source?.status === "ok" ? source.data : null;
  return (
    <>
      <PageHeader
        title="New agent"
        description="Start blank or from a template, then adjust before creating."
      />
      {organization.status === "ok" && hosted.status === "ok" && (
        <section className="flex flex-col gap-3">
          <h2 className="font-heading text-lg font-semibold">
            From the control service
          </h2>
          <CreateFromTemplate
            org={org}
            orgGrn={organization.data.grn}
            hosted={hostedFor(hosted.data, "agent:create")}
            controllers={Object.fromEntries(
              hosted.operators.map((o) => [o.grn, o.name]),
            )}
          />
        </section>
      )}
      <h2 className="font-heading text-lg font-semibold">Define in garam</h2>
      <section className="flex flex-col gap-2">
        <h2 className="text-sm font-medium">Start from</h2>
        {templates.status === "forbidden" ? (
          <p className="text-sm text-muted-foreground">
            Templates are not listed to you.
          </p>
        ) : null}
        <nav aria-label="Start from" className="flex flex-wrap gap-2">
          <StartLink href={base} current={template === null}>
            Blank
          </StartLink>
          {(templates.status === "ok" ? templates.data : []).map((t) => {
            const id = slugFromGrn(t.grn);
            // Names repeat, so the identifier stands beside each one.
            return (
              <StartLink
                key={t.grn}
                href={`${base}?template=${encodeURIComponent(id)}`}
                current={template === id}
              >
                {t.name}{" "}
                <span className="font-mono text-xs opacity-70">{id}</span>
              </StartLink>
            );
          })}
        </nav>
        {source?.status === "forbidden" && (
          <p role="alert" className="text-sm text-muted-foreground">
            garam refused to show that template, so the form starts blank.
          </p>
        )}
        {start && (
          <p className="text-sm text-muted-foreground">
            Copied from {start.name}. Changing the template later does not
            change this agent.
          </p>
        )}
      </section>
      <DefineAgentForm
        // A new start is a new form: the copy is taken once, when it
        // mounts.
        key={template ?? ""}
        org={org}
        operators={
          operators.status === "ok"
            ? operators.data.map((operator) => ({
                identifier: slugFromGrn(operator.grn),
                name: operator.name,
              }))
            : null
        }
        values={start?.values ?? {}}
      />
    </>
  );
}

function StartLink({
  href,
  current,
  children,
}: {
  href: string;
  current: boolean;
  children: React.ReactNode;
}) {
  return (
    <Link
      href={href}
      aria-current={current ? "page" : undefined}
      className={cn(
        "rounded-md border px-3 py-1.5 text-sm",
        current
          ? "border-primary bg-primary text-primary-foreground"
          : "hover:bg-muted",
      )}
    >
      {children}
    </Link>
  );
}
