import { CopyField } from "@/components/copy-field";
import { DescriptionList } from "@/components/description-list";
import { StatusDot, type StatusTone } from "@/components/status-dot";
import { Card } from "@/components/ui/card";

import { GrantFromCard } from "./grant-from-card";
import { WithdrawConsentDialog } from "./withdraw-consent-dialog";

/**
 * Where a pair stands, as one of the three states garam can serve. A
 * relation with neither half standing is deleted (ADR-0035), so the
 * two halves leave exactly these.
 */
type Pair = "crossing" | "awaiting-here" | "awaiting-there";

function pairOf(grantedHere: boolean, stands: boolean): Pair {
  if (stands) return "crossing";
  return grantedHere ? "awaiting-there" : "awaiting-here";
}

const PAIR: Record<
  Pair,
  { tone: StatusTone; label: (other: string) => string }
> = {
  crossing: { tone: "ok", label: () => "Messages cross" },
  "awaiting-here": {
    tone: "warn",
    label: () => "Waiting for this organization",
  },
  "awaiting-there": { tone: "info", label: (other) => `Waiting for ${other}` },
};

/** What the pair's state means for messages, in one sentence. */
function summary(pair: Pair, other: string): string {
  switch (pair) {
    case "crossing":
      return `Both halves stand, so messages cross between this organization and ${other} in either direction.`;
    case "awaiting-there":
      return `This organization has granted its half. Messages stay refused until ${other} grants its own.`;
    case "awaiting-here":
      return `${other} has granted its half. Messages stay refused until this organization grants its own.`;
  }
}

function Half({ granted }: { granted: boolean }) {
  return granted ? (
    <StatusDot tone="ok" label="Granted" />
  ) : (
    <StatusDot tone="neutral" label="Not granted" />
  );
}

/**
 * One consent relation: the other organization, where the pair stands
 * and each half of it. A relation is held by both sides and not
 * switched on by one (ADR-0035), so both halves are named, and only
 * this organization's half is granted or withdrawn here — from the card
 * itself, so a waiting pair is answered without typing the other
 * organization again.
 */
export function ConsentCard({
  org,
  other,
  grantedHere,
  grantedThere,
  stands,
  mayGrant,
  mayWithdraw,
}: {
  org: string;
  /** The other organization's identifier. */
  other: string;
  grantedHere: boolean;
  grantedThere: boolean;
  stands: boolean;
  mayGrant: boolean;
  mayWithdraw: boolean;
}) {
  const pair = pairOf(grantedHere, stands);
  const status = PAIR[pair];
  return (
    <Card
      role="group"
      aria-label={`Consent with ${other}`}
      className="w-full gap-4 p-4"
    >
      <div className="flex flex-wrap items-start justify-between gap-3">
        <CopyField label="Other organization" value={other} />
        <StatusDot tone={status.tone} label={status.label(other)} />
      </div>
      <DescriptionList
        items={[
          {
            term: "This organization",
            description: <Half granted={grantedHere} />,
          },
          {
            term: "The other organization",
            description: <Half granted={grantedThere} />,
          },
        ]}
      />
      <p className="text-muted-foreground">{summary(pair, other)}</p>
      {pair === "awaiting-here" && mayGrant && (
        <GrantFromCard org={org} other={other} />
      )}
      {grantedHere && mayWithdraw && (
        <WithdrawConsentDialog org={org} other={other} stands={stands} />
      )}
    </Card>
  );
}
