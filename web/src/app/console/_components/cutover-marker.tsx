import { Badge } from "@/components/ui/badge";
import type { AgentDefinition } from "@/features/orgs";

/** Where a definition's cutover stands, null where none is open. */
type Cutover = AgentDefinition["cutover"];

const STATE = {
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
  return <Badge variant="outline">{STATE[cutover.stage]}</Badge>;
}

/**
 * What a definition page says of values its operator no longer reads
 * (ADR-0086): garam keeps them as they were frozen and changes nothing
 * but the name.
 */
export function ArchivalNotice({ cutover }: { cutover: NonNullable<Cutover> }) {
  return (
    <p className="max-w-2xl rounded-md border px-3 py-2 text-sm">
      {STATE[cutover.stage]}. These settings are archival and read-only in
      garam; only the name still changes here.
    </p>
  );
}
