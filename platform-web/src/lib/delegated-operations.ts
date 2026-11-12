import type { components } from "@garam/platform-sdk-ts/api";

/** One operation a hosted operator's grant can carry. */
type DelegatedOperation = components["schemas"]["DelegatedOperation"];

/**
 * What each operation a grant can carry is called wherever a sentence
 * names it. A raw action string is never read by a person, so every
 * operation has its words here and the console spells none twice.
 */
export const DELEGATED_OPERATION_LABEL: Record<DelegatedOperation, string> = {
  "iam:agent.create": "Create agents from published templates",
  "iam:agent.configure": "Configure an agent's execution",
  "iam:agent.activate": "Activate an agent's runtime generation",
  "iam:agent.recover": "Recover an agent's credential",
  "iam:agent-template.read": "Read published templates",
  "iam:agent-template.publish": "Publish template versions",
  "iam:execution-profile.read": "Read execution profiles",
  "iam:agent.execution-read": "Read an agent's execution",
  "iam:operator.read": "Read operator status",
  "iam:agent.replace": "Move an agent to another controller",
};
