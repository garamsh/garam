import type { Metadata } from "next";
import Link from "next/link";

import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import {
  getOrg,
  listAgents,
  listGroups,
  listOperators,
  listOrgMembers,
  listProjects,
  type Agent,
  type Operator,
} from "@/features/orgs";

import { ApiUnreachable } from "../../_components/api-unreachable";
import { PageHeader } from "../../_components/page-header";
import { CONSOLE_SECTIONS, orgHref } from "../../_components/navigation";

type Params = { params: Promise<{ org: string }> };

/** Titles the route from the URL: the org's name needs a read. */
export async function generateMetadata({ params }: Params): Promise<Metadata> {
  const { org } = await params;
  return { title: `${org} — garam` };
}

/**
 * What one section's card carries: how many the section holds, and the
 * states inside it a reader has to act on. A count on its own reads as
 * the whole answer, so a section with a failing state says so here
 * rather than only on its own page.
 */
type Summary = { count: number; attention: string[] };

/**
 * The operator states worth surfacing. `expired` is the one nobody can
 * act on from the operators page — the token was lost or ran out — so
 * the overview is where it has to be visible; `pending` is a
 * registration somebody is still working through and is not one.
 */
function operatorAttention(operators: Operator[]): string[] {
  const expired = operators.filter(
    (operator) => operator.enrollmentState === "expired",
  ).length;
  return expired === 0 ? [] : [`${expired} expired`];
}

/**
 * The agent states worth surfacing, counted separately because the
 * operator and the agent report separately and neither answer
 * substitutes for the other (ADR-0053). One agent can be in both
 * counts, which is exactly what a single "not running" number would
 * hide. Each word is the one its badge carries on the agents page.
 */
function agentAttention(agents: Agent[]): string[] {
  const failed = agents.filter(
    (agent) => agent.provisioningState === "failed",
  ).length;
  const disconnected = agents.filter(
    (agent) => agent.runtimeState === "disconnected",
  ).length;
  return [
    ...(failed === 0 ? [] : [`${failed} failed`]),
    ...(disconnected === 0 ? [] : [`${disconnected} disconnected`]),
  ];
}

/** What the organization holds, counted, one card per section. */
export default async function OrgPage({ params }: Params) {
  const { org: slug } = await params;
  const [org, groups, projects, operators, agents, members] = await Promise.all(
    [
      getOrg(slug),
      listGroups(slug),
      listProjects(slug),
      listOperators(slug),
      listAgents(slug),
      listOrgMembers(slug),
    ],
  );

  if (
    org.status === "unreachable" ||
    groups.status === "unreachable" ||
    projects.status === "unreachable" ||
    operators.status === "unreachable" ||
    agents.status === "unreachable" ||
    members.status === "unreachable"
  ) {
    // The organization's own read may be the one that did not answer,
    // so the title comes from the URL slug rather than from its name.
    return (
      <>
        <PageHeader title={slug} description="What this organization holds." />
        <ApiUnreachable subject="what this organization holds" />
      </>
    );
  }

  const summaries = new Map<string, Summary>([
    ["teams", { count: groups.data.length, attention: [] }],
    ["projects", { count: projects.data.length, attention: [] }],
    [
      "operators",
      {
        count: operators.data.length,
        attention: operatorAttention(operators.data),
      },
    ],
    [
      "agents",
      { count: agents.data.length, attention: agentAttention(agents.data) },
    ],
    ["people", { count: members.data.length, attention: [] }],
  ]);
  // Label, icon and order come from the sidebar's own list, so a card
  // cannot drift from the section it links to.
  const cards = CONSOLE_SECTIONS.flatMap((section) => {
    const summary = summaries.get(section.segment);
    return summary === undefined ? [] : [{ ...section, ...summary }];
  });

  return (
    <>
      <PageHeader
        title={org.data.name}
        description="What this organization holds."
      />
      <ul className="grid gap-4 sm:grid-cols-3">
        {cards.map((card) => (
          <li key={card.segment}>
            <Link href={orgHref(slug, card.segment)} className="block">
              <Card className="gap-2 p-5 transition-colors hover:border-ring">
                <CardDescription className="flex items-center gap-2">
                  <card.icon className="size-4" />
                  {card.label}
                </CardDescription>
                <CardTitle className="text-3xl tabular-nums">
                  {card.count}
                </CardTitle>
                {card.attention.length > 0 && (
                  <p className="text-sm text-destructive tabular-nums">
                    {card.attention.join(" · ")}
                  </p>
                )}
              </Card>
            </Link>
          </li>
        ))}
      </ul>
      <p className="max-w-2xl text-sm text-muted-foreground">
        Consents and Permissions are not counted here. Reading consent relations
        and policies requires the admin role, which not every member of this
        organization holds.
      </p>
    </>
  );
}
