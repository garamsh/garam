import type { Agent, AgentDefinition } from "./api/orgs";
import { slugFromGrn } from "@/lib/grn";
import type { Catalog, Lookup } from "./subject";

/**
 * The two lists an agent can be known by: what operators reported and
 * what the organization defined (ADR-0033). A defined agent has its
 * GRN before any operator claims it, so it can be named, added to a
 * team or given a policy while still awaiting one (ADR-0075).
 */
export type AgentDirectory = {
  agents: Catalog<Agent>;
  definitions: Catalog<AgentDefinition>;
};

/**
 * Whether an operator has reported the agent: yes, no, or unknown
 * because the reported list was refused.
 */
export type Reported = "reported" | "awaiting" | "unchecked";

/** One agent as the directory knows it. */
export type DirectoryAgent = {
  grn: string;
  identifier: string;
  /** What the organization calls it, null where no definition names it. */
  name: string | null;
  reported: Reported;
};

/**
 * Every agent either list holds, defined ones first in the order the
 * definitions came, then reported ones no definition names.
 */
export function directoryAgents(directory: AgentDirectory): DirectoryAgent[] {
  const reported =
    directory.agents.status === "ok"
      ? new Set(directory.agents.data.map((agent) => agent.grn))
      : null;
  const reportedOf = (grn: string): Reported =>
    reported === null
      ? "unchecked"
      : reported.has(grn)
        ? "reported"
        : "awaiting";
  const defined =
    directory.definitions.status === "ok" ? directory.definitions.data : [];
  const named = new Set(defined.map((definition) => definition.agentGrn));
  return [
    ...defined.map((definition) => ({
      grn: definition.agentGrn,
      identifier: slugFromGrn(definition.agentGrn),
      name: definition.name,
      reported: reportedOf(definition.agentGrn),
    })),
    ...(directory.agents.status === "ok" ? directory.agents.data : [])
      .filter((agent) => !named.has(agent.grn))
      .map((agent) => ({
        grn: agent.grn,
        identifier: slugFromGrn(agent.grn),
        name: null,
        reported: "reported" as const,
      })),
  ];
}

/**
 * Looks an agent up by GRN. Found in either list is found; in neither
 * is missing only where both lists were read, and refused otherwise.
 */
export function lookUpAgent(
  directory: AgentDirectory,
  grn: string,
): { lookup: Lookup; agent?: DirectoryAgent } {
  const agent = directoryAgents(directory).find(
    (candidate) => candidate.grn === grn,
  );
  if (agent !== undefined) return { lookup: "found", agent };
  const refused =
    directory.agents.status === "forbidden" ||
    directory.definitions.status === "forbidden";
  return { lookup: refused ? "refused" : "missing" };
}

/**
 * What an agent garam holds no name for is called: one a control
 * service created has no definition, and its identifier never stands in
 * a name's place.
 */
export const UNNAMED_AGENT = "Unnamed agent";

/**
 * How an agent is offered in a selector: its name with the identifier
 * beside it, since names repeat and a selector offers nothing else to
 * tell two apart, and a note where no operator has reported it yet.
 */
export function agentChoiceLabel(agent: DirectoryAgent): string {
  const base = `${agent.name ?? UNNAMED_AGENT} (${agent.identifier})`;
  return agent.reported === "awaiting" ? `${base} · awaiting operator` : base;
}

/** The lists of the directory garam refused the caller, as plural nouns. */
export function refusedAgentLists(directory: AgentDirectory): string[] {
  return [
    ...(directory.definitions.status === "forbidden"
      ? ["agent definitions"]
      : []),
    ...(directory.agents.status === "forbidden" ? ["reported agents"] : []),
  ];
}

/** Whether what was reported needs someone's attention. */
export function isFailing(agent: Agent | undefined): boolean {
  return (
    agent?.provisioningState === "failed" ||
    agent?.runtimeState === "disconnected"
  );
}
