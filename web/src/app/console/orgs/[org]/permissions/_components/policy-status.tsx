import { StatusDot, type StatusTone } from "@/components/status-dot";
import type { PolicyStatus } from "@/features/orgs";

/** A policy's lifecycle state, as the word the console shows. */
export const POLICY_STATUS_LABEL: Record<PolicyStatus, string> = {
  draft: "Draft",
  active: "Active",
  archived: "Archived",
};

// Only an active policy takes part in checks, so only it reads as live;
// a draft and an archived one are both inert.
const TONE: Record<PolicyStatus, StatusTone> = {
  active: "ok",
  draft: "neutral",
  archived: "neutral",
};

/** A policy's lifecycle state, as a dot and its word. */
export function PolicyStatus({ status }: { status: PolicyStatus }) {
  return <StatusDot tone={TONE[status]} label={POLICY_STATUS_LABEL[status]} />;
}
