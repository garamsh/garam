import type { Metadata } from "next";
import Link from "next/link";

import { Banner } from "@/components/banner";
import { StatusDot, type StatusTone } from "@/components/status-dot";
import { Card } from "@/components/ui/card";
import {
  getOrg,
  listAgents,
  listGroups,
  listOperators,
  listOrgMembers,
  listPolicies,
  listProjects,
  lookupOrgConsents,
  type Agent,
  type Operator,
  type OrgConsent,
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

const DESCRIPTION = "What this organization holds, and what needs attention.";

/** One state inside a section that a count on its own would hide. */
type Signal = { tone: StatusTone; label: string };

/**
 * What one section's card carries: how many the section holds and the
 * states inside it a reader has to act on, or the fact that the caller
 * was refused the read.
 */
type Summary =
  | { status: "counted"; count: number; signals: Signal[] }
  | { status: "forbidden" };

/** A signal for `n` of something, or none when there are none. */
function counted(n: number, tone: StatusTone, label: string): Signal[] {
  return n === 0 ? [] : [{ tone, label: `${n} ${label}` }];
}

/**
 * The operator states worth surfacing. `expired` is the one nobody can
 * act on from the operators page — the token was lost or ran out — so
 * the overview is where it has to be visible; `pending` is a
 * registration somebody is still working through and is not one.
 */
function operatorSignals(operators: Operator[]): Signal[] {
  return counted(
    operators.filter((operator) => operator.enrollmentState === "expired")
      .length,
    "danger",
    "expired",
  );
}

/**
 * The agent states worth surfacing, counted separately because the
 * operator and the agent report separately and neither answer
 * substitutes for the other (ADR-0053). One agent can be in both
 * counts, which is exactly what a single "not running" number would
 * hide. Each word is the one its status carries on the agents page.
 */
function agentSignals(agents: Agent[]): Signal[] {
  return [
    ...counted(
      agents.filter((agent) => agent.provisioningState === "failed").length,
      "danger",
      "failed",
    ),
    ...counted(
      agents.filter((agent) => agent.runtimeState === "disconnected").length,
      "warn",
      "disconnected",
    ),
  ];
}

/**
 * A pair the other organization has granted and this one has not is
 * waiting on this organization alone, so it is worth a look; nothing
 * about it is failing.
 */
function consentSignals(consents: OrgConsent[]): Signal[] {
  return counted(
    consents.filter((consent) => !consent.grantedHere).length,
    "info",
    "waiting for this organization",
  );
}

/** Tones that mean something has failed, as opposed to waiting. */
const FAILING: ReadonlySet<StatusTone> = new Set(["danger", "warn"]);

/**
 * The organization's overview. It leads with what is failing, named
 * by section so it can be followed, then one card per section with its
 * count. The sections every member reads answer not-found when refused,
 * as their own pages do (ADR-0011); Consents and Permissions need more
 * than membership, so a refusal of either is said on its card.
 */
export default async function OrgPage({ params }: Params) {
  const { org: slug } = await params;
  const [
    org,
    groups,
    projects,
    operators,
    agents,
    members,
    consents,
    policies,
  ] = await Promise.all([
    getOrg(slug),
    listGroups(slug),
    listProjects(slug),
    listOperators(slug),
    listAgents(slug),
    listOrgMembers(slug),
    lookupOrgConsents(slug),
    listPolicies(slug),
  ]);

  if (
    org.status === "unreachable" ||
    groups.status === "unreachable" ||
    projects.status === "unreachable" ||
    operators.status === "unreachable" ||
    agents.status === "unreachable" ||
    members.status === "unreachable" ||
    consents.status === "unreachable" ||
    policies.status === "unreachable"
  ) {
    // The organization's own read may be the one that did not answer,
    // so the title comes from the URL slug rather than from its name.
    return (
      <div className="flex flex-col gap-6">
        <PageHeader title={slug} description={DESCRIPTION} />
        <ApiUnreachable subject="what this organization holds" />
      </div>
    );
  }

  const summaries = new Map<string, Summary>([
    ["teams", { status: "counted", count: groups.data.length, signals: [] }],
    ["people", { status: "counted", count: members.data.length, signals: [] }],
    [
      "agents",
      {
        status: "counted",
        count: agents.data.length,
        signals: agentSignals(agents.data),
      },
    ],
    [
      "projects",
      { status: "counted", count: projects.data.length, signals: [] },
    ],
    [
      "operators",
      {
        status: "counted",
        count: operators.data.length,
        signals: operatorSignals(operators.data),
      },
    ],
    [
      "consents",
      consents.status === "forbidden"
        ? { status: "forbidden" }
        : {
            status: "counted",
            count: consents.data.length,
            signals: consentSignals(consents.data),
          },
    ],
    [
      "permissions",
      policies.status === "forbidden"
        ? { status: "forbidden" }
        : { status: "counted", count: policies.data.length, signals: [] },
    ],
  ]);
  // Label, icon and order come from the sidebar's own list, so a card
  // cannot drift from the section it links to.
  const cards = CONSOLE_SECTIONS.flatMap((section) => {
    const summary = summaries.get(section.segment);
    return summary === undefined ? [] : [{ ...section, summary }];
  });
  const failing = cards.flatMap((card) =>
    card.summary.status === "counted"
      ? card.summary.signals
          .filter((signal) => FAILING.has(signal.tone))
          .map((signal) => ({ card, signal }))
      : [],
  );

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={org.data.name} description={DESCRIPTION} />
      {failing.length > 0 ? (
        <Banner
          tone="danger"
          title={
            failing.length === 1
              ? "1 state needs attention"
              : `${failing.length} states need attention`
          }
        >
          <ul className="flex flex-col gap-1.5 pt-1">
            {failing.map(({ card, signal }) => (
              <li key={`${card.segment} ${signal.label}`}>
                <Link
                  href={orgHref(slug, card.segment)}
                  className="inline-flex flex-wrap items-center gap-x-1 rounded-sm text-foreground underline-offset-4 outline-none hover:underline focus-visible:ring-3 focus-visible:ring-ring/50"
                >
                  <StatusDot
                    tone={signal.tone}
                    label={signal.label}
                    className="tabular-nums"
                  />
                  {/* A text node, so "failed in" stays two words when read. */}{" "}
                  <span>in {card.label}</span>
                </Link>
              </li>
            ))}
          </ul>
        </Banner>
      ) : (
        <Banner tone="ok" title="Nothing is failing">
          No operator&apos;s enrollment has expired, and no agent reports failed
          or disconnected.
        </Banner>
      )}
      <ul className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {cards.map((card) => (
          <li key={card.segment} className="flex">
            <Link
              href={orgHref(slug, card.segment)}
              className="flex w-full rounded-xl outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
            >
              <Card className="w-full gap-2 p-4 transition-colors hover:border-ring">
                <div className="flex items-center gap-2 text-muted-foreground">
                  <card.icon aria-hidden className="size-4" />
                  {card.label}
                </div>
                {card.summary.status === "counted" ? (
                  <>
                    <div className="text-2xl font-semibold tabular-nums">
                      {card.summary.count}
                    </div>
                    {card.summary.signals.map((signal) => (
                      <div key={signal.label}>
                        <StatusDot
                          tone={signal.tone}
                          label={signal.label}
                          className="tabular-nums"
                        />
                      </div>
                    ))}
                  </>
                ) : (
                  <div className="text-muted-foreground">
                    Not counted: the server did not let you read them.
                  </div>
                )}
              </Card>
            </Link>
          </li>
        ))}
      </ul>
    </div>
  );
}
