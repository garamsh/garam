import Link from "next/link";

import { Banner } from "@/components/banner";
import { Button } from "@/components/ui/button";
import { DELEGATED_OPERATION_LABEL } from "@/lib/delegated-operations";
import type { components } from "@/types/api";

/** One operation a hosted operator's grant can carry. */
type DelegatedOperation = components["schemas"]["DelegatedOperation"];

/**
 * Where what blocks a panel is fixed: the page of the operator whose
 * endpoint or grant falls short, the Operators list where no operator
 * has an endpoint, or nowhere the caller can open — a service that is
 * down, a refusal, an operators list the caller was not shown.
 */
export type ControlFix =
  { at: "operator"; identifier: string } | { at: "operators" } | { at: "none" };

/** A fix no page the caller can open makes. */
export const NO_FIX: ControlFix = { at: "none" };

/** Why a control service panel cannot act, and where that is fixed. */
export type ControlBlock = { reason: string; fix: ControlFix };

/**
 * What a panel reads of a hosted operator to say whether it can act:
 * where its page is, what it is called, the operations its grant
 * carries or null where the grant was not read, and why not.
 */
export type ReachableOperator = {
  identifier: string;
  name: string;
  delegation: { operations: readonly DelegatedOperation[] } | null;
  problem: string | null;
};

/** Whether a control service panel can act, and through whom. */
export type ControlReach<H extends ReachableOperator> =
  | { status: "blocked"; block: ControlBlock }
  | {
      status: "ready";
      hosted: H & { delegation: NonNullable<H["delegation"]> };
    };

/**
 * Whether a panel can reach a control service for every one of
 * `operations`: blocked where no operator has a registered endpoint,
 * or its delegation is unread or does not carry one of them. Everything
 * else on the page stays available either way; only the panel is
 * disabled.
 */
export function controlReach<H extends ReachableOperator>(
  hosted: H | null,
  ...operations: DelegatedOperation[]
): ControlReach<H> {
  const blocked = (reason: string, fix: ControlFix): ControlReach<H> => ({
    status: "blocked",
    block: { reason, fix },
  });
  // The operators were read to find the hosted one, so their list and
  // each operator's page are pages the caller can open.
  if (hosted === null) {
    return blocked(
      "No control service is registered. Register one on an operator's page.",
      { at: "operators" },
    );
  }
  const fix: ControlFix = { at: "operator", identifier: hosted.identifier };
  if (hosted.problem !== null) return blocked(hosted.problem, fix);
  const { delegation } = hosted;
  if (delegation === null) {
    return blocked(`${hosted.name} holds no delegation.`, fix);
  }
  const missing = operations.find(
    (operation) => !delegation.operations.some((op) => op === operation),
  );
  if (missing !== undefined) {
    return blocked(
      `${hosted.name}'s delegation does not include “${DELEGATED_OPERATION_LABEL[missing]}”.`,
      fix,
    );
  }
  return { status: "ready", hosted: { ...hosted, delegation } };
}

/** Why a panel cannot reach `operation`, or null where it can. */
export function controlBlocker(
  hosted: ReachableOperator | null,
  operation: DelegatedOperation,
): ControlBlock | null {
  const reach = controlReach(hosted, operation);
  return reach.status === "blocked" ? reach.block : null;
}

/**
 * What a disabled control service panel shows: the reason, and a link
 * to the page that fixes it where one does. `operatorsHref` is the
 * organization's Operators list, which the calling page names.
 */
export function ControlNotice({
  operatorsHref,
  reason,
  fix,
}: { operatorsHref: string } & ControlBlock) {
  return (
    <Banner
      tone="warn"
      title="The control service can't be used here"
      action={
        fix.at === "none" ? undefined : (
          <Button asChild variant="outline" size="sm">
            <Link
              href={
                fix.at === "operators"
                  ? operatorsHref
                  : `${operatorsHref}/${fix.identifier}`
              }
            >
              {fix.at === "operators" ? "Open operators" : "Open the operator"}
            </Link>
          </Button>
        )
      }
    >
      {reason}
    </Banner>
  );
}
