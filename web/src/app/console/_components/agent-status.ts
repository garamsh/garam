import type { StatusTone } from "@/components/status-dot";
import type { Agent, AgentDefinition, Reported } from "@/features/orgs";
import { slugFromGrn } from "@/lib/grn";

import { CUTOVER_STAGE_LABEL } from "./cutover-marker";

/** A state as a status dot draws it: what it means and what it says. */
export type AgentDot = { tone: StatusTone; label: string };

/**
 * What the operator reports about the pod. Only a state that needs
 * attention is drawn as danger, so a column of agents is read by its
 * dots rather than word by word.
 */
const PROVISIONING: Record<Agent["provisioningState"], AgentDot> = {
  pending: { tone: "info", label: "Provisioning" },
  provisioned: { tone: "info", label: "Provisioned" },
  ready: { tone: "ok", label: "Ready" },
  failed: { tone: "danger", label: "Provisioning failed" },
};

/**
 * What the agent reports about itself. `unknown` is an agent that has
 * not reported since it was created.
 */
const RUNTIME: Record<Agent["runtimeState"], AgentDot> = {
  unknown: { tone: "neutral", label: "No runtime report" },
  connected: { tone: "ok", label: "Connected" },
  disconnected: { tone: "danger", label: "Disconnected" },
};

const NOT_CHECKED: AgentDot = { tone: "neutral", label: "Not checked" };

/**
 * The two states operators and the agent itself reported, kept apart
 * and neither summarized into the other (ADR-0053). An agent no
 * operator has claimed has no reported state at all, which is said
 * rather than drawn as `pending`; a refused list is not checked.
 */
export function reportedDots(
  reported: Reported,
  agent: Agent | undefined,
): { provisioning: AgentDot; runtime: AgentDot } {
  if (reported === "unchecked") {
    return { provisioning: NOT_CHECKED, runtime: NOT_CHECKED };
  }
  if (reported === "awaiting" || agent === undefined) {
    return {
      provisioning: { tone: "neutral", label: "Awaiting operator" },
      runtime: { tone: "neutral", label: "No report yet" },
    };
  }
  return {
    provisioning: PROVISIONING[agent.provisioningState],
    runtime: RUNTIME[agent.runtimeState],
  };
}

/**
 * What the organization asked for: a definition, one cutting over or
 * cut over to the Operator (ADR-0086), or none — said as not checked
 * where the definitions were refused, since one may exist unread.
 */
export function requestedDot(
  definition: Pick<AgentDefinition, "cutover"> | undefined,
  definitionsRead: boolean,
): AgentDot {
  if (definition === undefined) {
    return definitionsRead
      ? { tone: "neutral", label: "No definition" }
      : NOT_CHECKED;
  }
  if (definition.cutover === null) return { tone: "ok", label: "Defined" };
  return {
    tone: definition.cutover.stage === "frozen" ? "warn" : "info",
    label: CUTOVER_STAGE_LABEL[definition.cutover.stage],
  };
}

/** An agent's one derived state, and the line saying what makes it so. */
export type AgentHealth = AgentDot & { detail: string };

/**
 * One state per agent for a list read at a glance, derived from the two
 * reports without replacing either (ADR-0053): the agent's own page
 * still shows both. A failure in either report leads.
 */
export function agentHealth(
  reported: Reported,
  agent: Agent | undefined,
): AgentHealth {
  if (reported === "unchecked") {
    return { ...NOT_CHECKED, detail: "Reported agents were not shown to you." };
  }
  if (reported === "awaiting" || agent === undefined) {
    return {
      tone: "neutral",
      label: "Not placed",
      detail: "Defined, and no operator or host holds it yet.",
    };
  }
  const provisioning = PROVISIONING[agent.provisioningState].label;
  const runtime = RUNTIME[agent.runtimeState].label;
  const detail = `${provisioning}; ${runtime.toLowerCase()}`;
  if (agent.provisioningState === "failed") {
    return { tone: "danger", label: "Failing", detail };
  }
  if (agent.runtimeState === "disconnected") {
    return { tone: "danger", label: "Not reporting", detail };
  }
  if (
    agent.provisioningState === "ready" &&
    agent.runtimeState === "connected"
  ) {
    return { tone: "ok", label: "Working", detail };
  }
  return { tone: "info", label: "Starting", detail };
}

/** What holds an agent, as its header and the list name it. */
export type Holder = {
  kind: "operator" | "host";
  /** The operator's name, or the host's identifier. */
  name: string;
  epoch: number;
  since: string;
};

/**
 * What holds the agent at its current epoch: an operator, named as the
 * organization calls it where its list was read, or a standalone
 * host's supervisor (ADR-0082). Null where nothing holds it.
 */
export function holderOf(
  agent: Agent | undefined,
  operatorNames: ReadonlyMap<string, string>,
): Holder | null {
  const assignment = agent?.assignment;
  if (assignment === undefined) return null;
  const { operatorGrn, assigneeGrn, epoch, since } = assignment;
  return operatorGrn === undefined
    ? { kind: "host", name: slugFromGrn(assigneeGrn), epoch, since }
    : {
        kind: "operator",
        name: operatorNames.get(operatorGrn) ?? slugFromGrn(operatorGrn),
        epoch,
        since,
      };
}

/** Whether a derived state is one a reader has to act on. */
export function isFailingHealth(state: AgentDot): boolean {
  return state.tone === "danger";
}
