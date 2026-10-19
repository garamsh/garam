import type { Metadata } from "next";
import Link from "next/link";

import { MemberMark } from "@/components/member-mark";
import { StatusDot, type StatusTone } from "@/components/status-dot";
import { Card } from "@/components/ui/card";
import { getSession } from "@/features/auth";
import {
  UNNAMED_AGENT,
  getOrg,
  isFailing,
  listGroupMembers,
  lookupAgentDefinitions,
  lookupAgents,
  lookupMyGroups,
  lookupOrgMembers,
  memberName,
  resolveTeamMembers,
  slugFromGrn,
  type Agent,
  type AgentDirectory,
  type Catalog,
  type Group,
  type Member,
  type TeamMember,
} from "@/features/orgs";

import { ApiUnreachable } from "../../../_components/api-unreachable";
import { orgHref } from "../../../_components/navigation";
import { PageHeader } from "../../../_components/page-header";
import { RefusedRead } from "../../../_components/refused-read";
import { Section } from "../../../_components/section";
import { homeLede, type LedeLink, type LedePart } from "./_components/lede";

type Params = { params: Promise<{ org: string }> };

/** Titles the route from the URL: the org's name needs a read. */
export async function generateMetadata({ params }: Params): Promise<Metadata> {
  const { org } = await params;
  return { title: `${org} — garam` };
}

/** A read Home shows a part of, or why it shows none. */
type Part<T> =
  | { status: "ok"; data: T }
  | { status: "forbidden" }
  | { status: "unreachable" };

/**
 * One of the caller's teams, with who is in it; `members` is null
 * where the team's members were not shown to the caller.
 */
type MyTeam = { group: Group; slug: string; members: TeamMember[] | null };

/** An agent the caller can read that needs attention. */
type Attention = {
  name: string;
  href: string;
  states: { tone: StatusTone; label: string }[];
};

/** The parts of an agent's report that need attention, in words. */
function agentStates(agent: Agent): Attention["states"] {
  return [
    ...(agent.provisioningState === "failed"
      ? [{ tone: "danger" as const, label: "Provisioning failed" }]
      : []),
    ...(agent.runtimeState === "disconnected"
      ? [{ tone: "danger" as const, label: "Disconnected" }]
      : []),
  ];
}

/**
 * Who is in each of the caller's teams: one members read per team the
 * caller is in, in parallel.
 */
async function readMyTeams(
  org: string,
  groups: readonly Group[],
  people: Catalog<Member>,
  directory: AgentDirectory,
): Promise<MyTeam[]> {
  const reads = await Promise.all(
    groups.map((group) => listGroupMembers(org, slugFromGrn(group.grn))),
  );
  return groups.map((group, index) => {
    const read = reads[index];
    return {
      group,
      slug: slugFromGrn(group.grn),
      members:
        read.status === "ok"
          ? resolveTeamMembers(read.data, people, directory)
          : null,
    };
  });
}

/** A read as a catalog to name members from; a failed one names none. */
function catalog<T>(read: Part<T[]>): Catalog<T> {
  return read.status === "ok"
    ? { status: "ok", data: read.data }
    : { status: "forbidden" };
}

/**
 * Home: the organization as the person signed in meets it (ADR-0113).
 * It opens with a sentence generated from what follows, then what is
 * waiting on them, their teams with who is in each, and the agents they
 * can read that are failing. Only what the browser API serves is shown:
 * nothing waits on anyone until garam has work items, and no activity
 * is invented where garam records none. The organization is read for
 * itself, so one the caller cannot read answers 404 (ADR-0011).
 */
export default async function HomePage({ params }: Params) {
  const { org: slug } = await params;
  const [org, session, groups, people, agents, definitions] = await Promise.all(
    [
      getOrg(slug),
      getSession(),
      lookupMyGroups(slug),
      lookupOrgMembers(slug),
      lookupAgents(slug),
      lookupAgentDefinitions(slug),
    ],
  );

  if (org.status === "unreachable") {
    return (
      <div className="flex flex-col gap-6">
        <PageHeader title={slug} description="Home" />
        <ApiUnreachable subject="this organization" />
      </div>
    );
  }

  const href = (segment: string) => orgHref(slug, segment);
  const me = session.status === "signed-in" ? session.user.grn : null;
  const directory = {
    agents: catalog(agents),
    definitions: catalog(definitions),
  };

  const teams: Part<MyTeam[]> =
    groups.status !== "ok"
      ? groups
      : {
          status: "ok",
          data: await readMyTeams(
            slug,
            groups.data,
            catalog(people),
            directory,
          ),
        };

  const named = new Map(
    definitions.status === "ok"
      ? definitions.data.map((definition) => [
          definition.agentGrn,
          definition.name,
        ])
      : [],
  );
  const attention: Attention[] = (agents.status === "ok" ? agents.data : [])
    .filter((agent) => isFailing(agent))
    .map((agent) => ({
      name: named.get(agent.grn) ?? UNNAMED_AGENT,
      href: href(`agents/${slugFromGrn(agent.grn)}`),
      states: agentStates(agent),
    }));

  const lede = homeLede({
    teams:
      teams.status === "ok"
        ? teams.data.map((team) => ({
            text: team.group.name,
            href: href(`teams/${team.slug}`),
          }))
        : null,
    attention:
      agents.status === "ok"
        ? attention.map((item) => ({ text: item.name, href: item.href }))
        : null,
  });

  return (
    <div className="flex flex-col gap-8">
      <PageHeader title={org.data.name} description={<Lede parts={lede} />} />

      <Section title="Needs you" count={0} region>
        <p className="rounded-xl border border-dashed p-4 text-muted-foreground">
          garam has no work items yet. Once it does, work handed to you and
          reviews you are asked for land here.
        </p>
      </Section>

      <Section
        title="Your teams"
        count={teams.status === "ok" ? teams.data.length : undefined}
        region
      >
        {teams.status === "unreachable" ? (
          <ApiUnreachable subject="your teams" />
        ) : teams.status === "forbidden" ? (
          <RefusedRead subject="this organization's teams" />
        ) : teams.data.length === 0 ? (
          <p className="text-muted-foreground">
            You are in no team in this organization.
          </p>
        ) : (
          <ul className="grid gap-4 lg:grid-cols-2">
            {teams.data.map((team) => (
              <li key={team.slug} className="flex">
                <TeamCard org={slug} team={team} me={me} />
              </li>
            ))}
          </ul>
        )}
      </Section>

      <Section
        title="Worth a look"
        description="What you can read in this organization that is failing."
        count={agents.status === "ok" ? attention.length : undefined}
        region
      >
        {agents.status !== "ok" ? (
          <RefusedRead subject="this organization's agents" />
        ) : attention.length === 0 ? (
          <p className="text-muted-foreground">
            Nothing you can read is failing.
          </p>
        ) : (
          <ul className="flex flex-col divide-y rounded-xl border">
            {attention.map((item) => (
              <li
                key={item.href}
                className="flex flex-wrap items-center gap-x-3 gap-y-1 px-4 py-2.5"
              >
                <MemberMark kind="agent" name={item.name} />
                <Link
                  href={item.href}
                  className="font-medium underline-offset-4 outline-none hover:underline focus-visible:ring-3 focus-visible:ring-ring/50"
                >
                  {item.name}
                </Link>
                <span className="text-muted-foreground">agent</span>
                <span className="flex flex-wrap gap-x-3 sm:ml-auto">
                  {item.states.map((state) => (
                    <StatusDot key={state.label} {...state} />
                  ))}
                </span>
              </li>
            ))}
          </ul>
        )}
      </Section>
    </div>
  );
}

/** The generated lede, its names linked. */
function Lede({ parts }: { parts: readonly LedePart[] }) {
  return (
    <>
      {parts.map((part, index) =>
        typeof part === "string" ? (
          part
        ) : (
          <LedeAnchor key={index} link={part} />
        ),
      )}
    </>
  );
}

function LedeAnchor({ link }: { link: LedeLink }) {
  return (
    <Link
      href={link.href}
      className="rounded-sm text-foreground underline underline-offset-4 outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
    >
      {link.text}
    </Link>
  );
}

/** One of the caller's teams and who is in it, people and agents. */
function TeamCard({
  org,
  team,
  me,
}: {
  org: string;
  team: MyTeam;
  me: string | null;
}) {
  return (
    <Card className="w-full gap-3 p-4">
      <Link
        href={orgHref(org, `teams/${team.slug}`)}
        className="flex items-center gap-2 self-start rounded-sm font-medium underline-offset-4 outline-none hover:underline focus-visible:ring-3 focus-visible:ring-ring/50"
      >
        <MemberMark kind="team" name={team.group.name} />
        {team.group.name}
      </Link>
      {team.members === null ? (
        <p className="text-muted-foreground">
          Its members were not shown to you.
        </p>
      ) : (
        <ul className="flex flex-col gap-2">
          {team.members.map((member) => (
            <li key={member.grn} className="flex items-center gap-2">
              {member.kind === "unknown" ? (
                <span aria-hidden className="size-6 shrink-0" />
              ) : (
                <MemberMark kind={member.kind} name={memberName(member)} />
              )}
              <span className="min-w-0 truncate">{memberName(member)}</span>
              <span className="text-muted-foreground">
                {member.grn === me
                  ? "you"
                  : member.kind === "unknown"
                    ? "unknown"
                    : member.kind}
              </span>
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}
