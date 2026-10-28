import { CopyField } from "@/components/copy-field";
import { DescriptionList } from "@/components/description-list";
import { StatusDot, type StatusTone } from "@/components/status-dot";
import { Card } from "@garam/design/ui/card";

import { grantOrgConsent, withdrawOrgConsent } from "@/features/orgs/actions";

import { ConsentConfirmDialog } from "./consent-confirm-dialog";

/**
 * Where a pair stands, as one of the three states garam can serve.
 * Nothing about a waiting pair is failing: the one waiting on this
 * organization, which it can act on, reads as info and the other as
 * neutral. A relation with neither half standing is deleted
 * (ADR-0035), so the two halves leave exactly these.
 */
type Pair = "crossing" | "awaiting-here" | "awaiting-there";

function pairOf(grantedHere: boolean, stands: boolean): Pair {
  if (stands) return "crossing";
  return grantedHere ? "awaiting-there" : "awaiting-here";
}

// The other organization is named by its identifier alone, which is
// often random hex, so no sentence or label carries it; the card's
// `CopyField` is the one place it is read (ADR-0103 §Principles).
const PAIR: Record<Pair, { tone: StatusTone; label: string }> = {
  crossing: { tone: "ok", label: "Messages cross" },
  "awaiting-here": { tone: "info", label: "Waiting for this organization" },
  "awaiting-there": {
    tone: "neutral",
    label: "Waiting for the other organization",
  },
};

/** What the pair's state means for messages, in one sentence. */
const SUMMARY: Record<Pair, string> = {
  crossing:
    "Both halves stand, so messages cross between the two organizations in either direction.",
  "awaiting-there":
    "This organization has granted its half. Messages stay refused until the other organization grants its own.",
  "awaiting-here":
    "The other organization has granted its half. Messages stay refused until this organization grants its own.",
};

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
      aria-label={`Other organization ${other}`}
      className="w-full gap-4 p-4"
    >
      <div className="flex flex-wrap items-start justify-between gap-3">
        <CopyField label="Other organization" value={other} />
        <StatusDot tone={status.tone} label={status.label} />
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
      <p className="text-muted-foreground">{SUMMARY[pair]}</p>
      {pair === "awaiting-here" && mayGrant && (
        <ConsentConfirmDialog
          org={org}
          other={other}
          action={grantOrgConsent}
          act="Grant consent"
          doing="Granting…"
          title="Grant this organization's consent?"
          consequence="The other organization already holds its half, so messages cross between the two organizations in either direction from then on."
        />
      )}
      {grantedHere && mayWithdraw && (
        <ConsentConfirmDialog
          org={org}
          other={other}
          action={withdrawOrgConsent}
          act="Withdraw consent"
          doing="Withdrawing…"
          title="Withdraw this organization's consent?"
          consequence={
            stands
              ? "Messages between the two organizations stop at once. The other organization keeps its own half, so granting again here would let them cross."
              : "Nothing crosses today, because the other organization has not granted its half, and withdrawing removes the relation."
          }
          destructive
        />
      )}
    </Card>
  );
}
