import { Badge } from "@/components/ui/badge";
import type { AgentDefinition } from "@/features/orgs";

/** Where a definition's cutover stands, null where none is open. */
type Cutover = AgentDefinition["cutover"];

/** What each open cutover stage is called wherever it is shown. */
export const CUTOVER_STAGE_LABEL = {
  switched: "Managed by the Operator",
  frozen: "Cut-over in progress",
} as const;

/**
 * Where a listed definition's cutover stands, or nothing where none is
 * open (ADR-0086). A rolled-back attempt is no open one, so garam reads
 * it as none and it is listed as before.
 */
export function CutoverBadge({ cutover }: { cutover: Cutover }) {
  if (cutover === null) return null;
  return <Badge variant="outline">{CUTOVER_STAGE_LABEL[cutover.stage]}</Badge>;
}
