import type { components } from "@/types/api";

/** One operation a hosted operator's grant can carry. */
type DelegatedOperation = components["schemas"]["DelegatedOperation"];

/**
 * What each operation a grant can carry is called wherever a sentence
 * names it. A raw action string is never read by a person, so every
 * operation has its words here and the console spells none twice.
 */
export const DELEGATED_OPERATION_LABEL: Record<DelegatedOperation, string> = {
  "agent:create": "Create agents from published templates",
  "agent:configure": "Configure an agent's execution",
  "agent:activate": "Activate an agent's runtime generation",
  "agent:recover": "Recover an agent's credential",
  "agent-template:read": "Read published templates",
  "agent-template:publish": "Publish template versions",
  "execution-profile:read": "Read execution profiles",
  "agent:execution-read": "Read an agent's execution",
  "agent:cutover": "Cut an agent over to the Operator",
  "operator:read": "Read operator status",
  "agent:replace": "Move an agent to another controller",
};
