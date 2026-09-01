import { Badge } from "@/components/ui/badge";
import { Card, CardTitle } from "@/components/ui/card";
import { slugFromGrn, type OrgConsent } from "@/features/orgs";

import { WithdrawConsentForm } from "./withdraw-consent-form";

/**
 * What the pair's state means for messages, in one sentence. A waiting
 * relation is the state most of them are in and the one the two halves
 * alone do not explain, so the sentence names the half that is missing.
 *
 * Every relation garam serves has a half standing, the row being
 * deleted when the last one falls (ADR-0035). The last case is
 * therefore the other organization's half standing alone.
 */
function summary(consent: OrgConsent, other: string): string {
  if (consent.stands) {
    return `Both halves stand, so messages cross between this organization and ${other} in either direction.`;
  }
  if (consent.grantedHere) {
    return `This organization has granted its half. Messages stay refused until ${other} grants its own.`;
  }
  return `${other} has granted its half. Messages stay refused until this organization grants its own.`;
}

/**
 * The organizations this one holds a consent relation with, and which
 * half of each pair stands. A relation is held by both sides and not
 * switched on by one (ADR-0035), so both halves are named on every row
 * and only the half this organization holds can be withdrawn here.
 */
export function ConsentList({
  org,
  consents,
}: {
  org: string;
  consents: OrgConsent[];
}) {
  return (
    <ul className="flex flex-col gap-3">
      {consents.map((consent) => {
        const other = slugFromGrn(consent.orgGrn);
        return (
          <li key={consent.orgGrn}>
            <Card className="gap-4 p-4">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <CardTitle className="font-mono text-base">{other}</CardTitle>
                <Badge variant={consent.stands ? "secondary" : "outline"}>
                  {consent.stands ? "Messages cross" : "Waiting"}
                </Badge>
              </div>
              <dl className="grid grid-cols-[auto_1fr] items-center gap-x-4 gap-y-1.5 text-sm">
                <dt className="text-muted-foreground">This organization</dt>
                <dd>{consent.grantedHere ? "Granted" : "Not granted"}</dd>
                <dt className="text-muted-foreground">
                  The other organization
                </dt>
                <dd>{consent.grantedThere ? "Granted" : "Not granted"}</dd>
              </dl>
              <p className="text-sm text-muted-foreground">
                {summary(consent, other)}
              </p>
              {consent.grantedHere && (
                <WithdrawConsentForm
                  org={org}
                  other={other}
                  stands={consent.stands}
                />
              )}
            </Card>
          </li>
        );
      })}
    </ul>
  );
}
