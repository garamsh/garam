import { Badge } from "@/components/ui/badge";
import type { Operator } from "@/features/orgs";

/** How a state is drawn, out of what the badge offers. */
type BadgeVariant = React.ComponentProps<typeof Badge>["variant"];

/**
 * Where the operator stands in obtaining a certificate, and the marking
 * each state takes. Only a state that needs attention is drawn loudly:
 * `expired` is the one nobody can act on from the console — the token
 * was lost or ran out — while `pending` is a registration somebody is
 * still working through.
 */
const ENROLLMENT_BADGE: Record<Operator["enrollmentState"], BadgeVariant> = {
  enrolled: "secondary",
  pending: "outline",
  expired: "destructive",
};

/**
 * An operator's enrollment state. It is what garam holds and never a
 * report from the operator, so it is labelled as enrollment wherever it
 * is drawn and never as status.
 */
export function EnrollmentBadge({
  state,
}: {
  state: Operator["enrollmentState"];
}) {
  return <Badge variant={ENROLLMENT_BADGE[state]}>{state}</Badge>;
}
