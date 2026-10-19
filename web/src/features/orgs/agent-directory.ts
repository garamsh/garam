import type { Agent } from "./api/orgs";
import { slugFromGrn } from "@/lib/grn";
import type { Catalog, Lookup } from "./subject";

/**
 * The list an agent is known by: the agents the organization holds.
 * garam keeps no agent definition (ADR-0118), so every agent it knows
 * is one an operator or a supervisor stood up.
 */
export type AgentDirectory = {
  agents: Catalog<Agent>;
};

/**
 * Whether the agent is known from the agents list: yes, or unknown
 * because that list was refused.
 */
export type Reported = "reported" | "unchecked";

/** One agent as the directory knows it. */
export type DirectoryAgent = {
  grn: string;
  identifier: string;
  /** What the organization calls it: garam holds no name for an agent. */
  name: string | null;
  reported: Reported;
};

/** Every agent the list holds, in the order it came. */
export function directoryAgents(directory: AgentDirectory): DirectoryAgent[] {
  return (directory.agents.status === "ok" ? directory.agents.data : []).map(
    (agent) => ({
      grn: agent.grn,
      identifier: slugFromGrn(agent.grn),
      name: null,
      reported: "reported" as const,
    }),
  );
}

/**
 * Looks an agent up by GRN. Not in the list is missing where the list
 * was read, and refused otherwise.
 */
export function lookUpAgent(
  directory: AgentDirectory,
  grn: string,
): { lookup: Lookup; agent?: DirectoryAgent } {
  const agent = directoryAgents(directory).find(
    (candidate) => candidate.grn === grn,
  );
  if (agent !== undefined) return { lookup: "found", agent };
  return {
    lookup: directory.agents.status === "forbidden" ? "refused" : "missing",
  };
}

/**
 * What an agent is called, since garam holds no name for one: its
 * identifier never stands in a name's place.
 */
export const UNNAMED_AGENT = "Unnamed agent";

/**
 * How an agent is offered in a selector: its name with the identifier
 * beside it, since names repeat and a selector offers nothing else to
 * tell two apart.
 */
export function agentChoiceLabel(agent: DirectoryAgent): string {
  return `${agent.name ?? UNNAMED_AGENT} (${agent.identifier})`;
}

/** Whether what was reported needs someone's attention. */
export function isFailing(agent: Agent | undefined): boolean {
  return (
    agent?.provisioningState === "failed" ||
    agent?.runtimeState === "disconnected"
  );
}
