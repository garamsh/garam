import { Badge } from "@/components/ui/badge";
import { Card, CardTitle } from "@/components/ui/card";
import { slugFromGrn, type Operator } from "@/features/orgs";

import { DeleteOperatorForm } from "./delete-operator-form";

/** How a state is drawn, out of what the badge offers. */
type BadgeVariant = React.ComponentProps<typeof Badge>["variant"];

/**
 * Where the operator stands in obtaining a certificate, and the marking
 * each state takes. Only a state that needs attention is drawn loudly:
 * `expired` is the one nobody can act on from here — the token was lost
 * or ran out, and the route that mints another (ADR-0067) has no
 * control on this page yet — while `pending` is a registration somebody
 * is still working through.
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
 *
 * `registeredBy` is a user GRN and the record carries no name beside
 * it (`api/openapi.yaml` §Operator), so the identifier is monospaced
 * as an address and the list says what it is rather than leaving an
 * identity subject to read as somebody's name.
 *
 * What deleting a record does is said under the records rather than
 * over the form that writes one: a reader meets it here, where the
 * list it describes and the control that does it are on screen.
 */
export function OperatorList({
  org,
  operators,
}: {
  org: string;
  operators: Operator[];
}) {
  return (
    <div className="flex flex-col gap-3">
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
                <dd className="font-mono">
                  {slugFromGrn(operator.registeredBy)}
                </dd>
                <dt className="text-muted-foreground">Registered</dt>
                <dd>
                  <time dateTime={operator.createdAt}>
                    {operator.createdAt.slice(0, 10)}
                  </time>
                </dd>
              </dl>
              <DeleteOperatorForm
                org={org}
                operator={slugFromGrn(operator.grn)}
                enrolled={operator.enrollmentState === "enrolled"}
              />
            </Card>
          </li>
        ))}
      </ul>
      <p className="max-w-2xl text-sm text-muted-foreground">
        Registered by is the user identifier the record holds; it carries no
        display name.
      </p>
      <p className="max-w-2xl text-sm text-muted-foreground">
        Deleting a record stops that operator: a handshake is checked against
        this record as well as against the certificate and this
        organization&apos;s certificate authority, so the certificate it holds
        is refused from its next connection. It is not a certificate revocation
        — nothing is published and no other certificate is affected, and agents
        the operator provisioned keep their own. garam does not bring a deleted
        record back, and an operator an agent definition names is refused until
        no definition names it.
      </p>
    </div>
  );
}
