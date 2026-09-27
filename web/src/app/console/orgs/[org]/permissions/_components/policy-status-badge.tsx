import { Badge } from "@/components/ui/badge";
import type { PolicyStatus } from "@/features/orgs";

/** A policy's lifecycle state, as the word the console shows. */
export const POLICY_STATUS_LABEL: Record<PolicyStatus, string> = {
  draft: "Draft",
  active: "Active",
  archived: "Archived",
};

const VARIANT: Record<PolicyStatus, "default" | "outline" | "secondary"> = {
  active: "default",
  draft: "outline",
  archived: "secondary",
};

/** A policy's lifecycle state, as one word. */
export function PolicyStatusBadge({ status }: { status: PolicyStatus }) {
  return <Badge variant={VARIANT[status]}>{POLICY_STATUS_LABEL[status]}</Badge>;
}
