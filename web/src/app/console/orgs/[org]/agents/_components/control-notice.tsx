import Link from "next/link";

import { Banner } from "@/components/banner";
import { Button } from "@/components/ui/button";
import type { DelegatedOperation, HostedOperator } from "@/features/orgs";
import { DELEGATED_OPERATION_LABEL } from "@/lib/delegated-operations";

import { orgHref } from "../../../../_components/navigation";

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
  ...operations: DelegatedOperation[]
): ControlReach {
  const blocked = (reason: string, fix: ControlFix): ControlReach => ({
    status: "blocked",
    block: { reason, fix },
  });
  // The operators were read to find the hosted one, so their list and
  // each operator's page are pages the caller can open.
  if (hosted === null) {
    return blocked(
      "No control service is registered for this organization. An administrator registers one on an operator's page.",
      { at: "operators" },
    );
  }
  const fix: ControlFix = { at: "operator", identifier: hosted.identifier };
  if (hosted.problem !== null) return blocked(hosted.problem, fix);
  const { delegation } = hosted;
  if (delegation === null) {
    return blocked(
      `${hosted.name} holds no delegation, so its control service is not reachable from here.`,
      fix,
    );
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
  hosted: HostedOperator | null,
  operation: DelegatedOperation,
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
  fix,
}: { org: string } & ControlBlock) {
  const operators = orgHref(org, "operators");
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
                  ? operators
                  : `${operators}/${fix.identifier}`
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
