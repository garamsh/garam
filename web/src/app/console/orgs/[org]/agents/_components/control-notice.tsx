import Link from "next/link";

import { Banner } from "@/components/banner";
import { Button } from "@/components/ui/button";
import type { HostedOperator } from "@/features/orgs";

import { orgHref } from "../../../../_components/navigation";

/**
 * Why a control service panel cannot act, and where that is fixed: the
 * page of the operator whose endpoint or grant falls short, or the
 * Operators list where no operator has an endpoint to fix.
 */
export type ControlBlock = {
  reason: string;
  /**
   * The operator whose page fixes it, null for the Operators list, and
   * absent where no page fixes it — a service that is down, a refusal.
   */
  fixAt?: string | null;
};

/** A hosted operator whose grant was read. */
export type DelegatedOperator = HostedOperator & {
  delegation: NonNullable<HostedOperator["delegation"]>;
};

/** Whether a control service panel can act, and through whom. */
export type ControlReach =
  | { status: "blocked"; block: ControlBlock }
  | { status: "ready"; hosted: DelegatedOperator };

/**
 * Whether a panel can reach a control service for every one of
 * `operations`: blocked where no operator has a registered endpoint,
 * or its delegation is unread or does not carry one of them. Everything
 * else on the page stays available either way; only the panel is
 * disabled.
 */
export function controlReach(
  hosted: HostedOperator | null,
  ...operations: string[]
): ControlReach {
  const blocked = (reason: string, fixAt: string | null): ControlReach => ({
    status: "blocked",
    block: { reason, fixAt },
  });
  if (hosted === null) {
    return blocked(
      "No control service is registered for this organization. An administrator registers one on an operator's page.",
      null,
    );
  }
  const fixAt = hosted.identifier;
  if (hosted.problem !== null) return blocked(hosted.problem, fixAt);
  const { delegation } = hosted;
  if (delegation === null) {
    return blocked(
      `${hosted.name} holds no delegation, so its control service is not reachable from here.`,
      fixAt,
    );
  }
  const missing = operations.find(
    (operation) => !delegation.operations.some((op) => op === operation),
  );
  if (missing !== undefined) {
    return blocked(
      `${hosted.name}'s delegation does not include ${missing}.`,
      fixAt,
    );
  }
  return { status: "ready", hosted: { ...hosted, delegation } };
}

/** Why a panel cannot reach `operation`, or null where it can. */
export function controlBlocker(
  hosted: HostedOperator | null,
  operation: string,
): ControlBlock | null {
  const reach = controlReach(hosted, operation);
  return reach.status === "blocked" ? reach.block : null;
}

/**
 * What a disabled control service panel shows: the reason, and a link
 * to the page that fixes it where one does.
 */
export function ControlNotice({
  org,
  reason,
  fixAt,
}: { org: string } & ControlBlock) {
  const operators = orgHref(org, "operators");
  return (
    <Banner
      tone="warn"
      title="The control service can't be used here"
      action={
        fixAt === undefined ? undefined : (
          <Button asChild variant="outline" size="sm">
            <Link href={fixAt === null ? operators : `${operators}/${fixAt}`}>
              {fixAt === null ? "Open operators" : "Open the operator"}
            </Link>
          </Button>
        )
      }
    >
      {reason}
    </Banner>
  );
}

/** What an unanswered control service looks like on every panel. */
export const UNAVAILABLE =
  "The control service is unavailable, so its controls are disabled. Teams, people and agents are unaffected.";
