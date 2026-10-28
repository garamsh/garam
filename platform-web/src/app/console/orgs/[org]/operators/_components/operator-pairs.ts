import type { Operator } from "@/features/orgs";
import { slugFromGrn } from "@/lib/grn";

/** One identity of an Operator as the list shows it. */
export type OperatorIdentity = {
  grn: string;
  identifier: string;
  name: string;
  controlEndpoint?: string;
};

/**
 * One Operator: its manager and the control service that names it
 * (ADR-0124). `grn` and `identifier` are the manager's, and key the row
 * and its reports. A row without a manager record is a control service
 * naming a manager garam no longer holds, so it always has the one.
 */
export type OperatorRow = { grn: string; identifier: string } & (
  | { manager: OperatorIdentity; controlService?: OperatorIdentity }
  | { manager?: undefined; controlService: OperatorIdentity }
);

function identityOf(operator: Operator): OperatorIdentity {
  return {
    grn: operator.grn,
    identifier: slugFromGrn(operator.grn),
    name: operator.name,
    controlEndpoint: operator.controlEndpoint,
  };
}

/**
 * The organization's operators as Operators: each record that names no
 * manager is one, joined by the record that names it, and a record
 * naming a manager the list does not hold is one on its own. The pair
 * is read from `manager` alone, never from names (ADR-0124).
 */
export function pairOperators(operators: Operator[]): OperatorRow[] {
  const held = new Set(operators.map((operator) => operator.grn));
  const controlOf = new Map(
    operators.flatMap((operator) =>
      operator.manager === undefined ? [] : [[operator.manager, operator]],
    ),
  );
  return operators.flatMap((operator): OperatorRow[] => {
    if (operator.manager === undefined) {
      const control = controlOf.get(operator.grn);
      return [
        {
          grn: operator.grn,
          identifier: slugFromGrn(operator.grn),
          manager: identityOf(operator),
          controlService: control && identityOf(control),
        },
      ];
    }
    if (held.has(operator.manager)) return [];
    return [
      {
        grn: operator.manager,
        identifier: slugFromGrn(operator.manager),
        controlService: identityOf(operator),
      },
    ];
  });
}
