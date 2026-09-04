import { Badge } from "@/components/ui/badge";
import { Card, CardTitle } from "@/components/ui/card";
import { slugFromGrn, type Operator } from "@/features/orgs";

/** How a state is drawn, out of what the badge offers. */
type BadgeVariant = React.ComponentProps<typeof Badge>["variant"];

/**
 * Where the operator stands in obtaining a certificate, and the marking
 * each state takes. Only a state that needs attention is drawn loudly:
 * `expired` is the one nobody can act on from here — the token was lost
 * or ran out and no route mints a second — while `pending` is a
 * registration somebody is still working through.
 */
const ENROLLMENT_BADGE: Record<Operator["enrollmentState"], BadgeVariant> = {
  enrolled: "secondary",
  pending: "outline",
  expired: "destructive",
};

/**
 * The organization's registered operators: what each is identified by,
 * where it stands in obtaining a certificate, and who registered it.
 *
 * The state is what garam already holds and not a report from the
 * operator — garam never dials one, so `enrolled` says a certificate
 * was obtained and nothing about whether the operator is running.
 * `Certificate` is when garam last signed for it, which a renewal
 * moves, so it is not when the operator enrolled.
 */
export function OperatorList({ operators }: { operators: Operator[] }) {
  return (
    <ul className="flex flex-col gap-3">
      {operators.map((operator) => (
        <li key={operator.grn}>
          <Card className="gap-4 p-4">
            <CardTitle className="font-mono text-base">
              {slugFromGrn(operator.grn)}
            </CardTitle>
            <dl className="grid grid-cols-[auto_1fr] items-center gap-x-4 gap-y-1.5 text-sm">
              <dt className="text-muted-foreground">Enrollment</dt>
              <dd>
                <Badge variant={ENROLLMENT_BADGE[operator.enrollmentState]}>
                  {operator.enrollmentState}
                </Badge>
              </dd>
              {operator.certificateIssuedAt && (
                <>
                  <dt className="text-muted-foreground">Certificate</dt>
                  <dd>
                    <time dateTime={operator.certificateIssuedAt}>
                      {operator.certificateIssuedAt.slice(0, 10)}
                    </time>
                  </dd>
                </>
              )}
              <dt className="text-muted-foreground">Registered by</dt>
              <dd>{slugFromGrn(operator.registeredBy)}</dd>
              <dt className="text-muted-foreground">Registered</dt>
              <dd>
                <time dateTime={operator.createdAt}>
                  {operator.createdAt.slice(0, 10)}
                </time>
              </dd>
            </dl>
          </Card>
        </li>
      ))}
    </ul>
  );
}
