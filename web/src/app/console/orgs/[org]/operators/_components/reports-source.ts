import { hostedFor, type HostedOperatorsRead } from "@/features/orgs";

import type { ReportsSource } from "./operator-reports";

/**
 * Where a page reads its operators' reports from: the hosted operator
 * whose grant carries `operator:read`, else the first, and the
 * organization's GRN its record names; or why the hosted operators
 * could not be read.
 */
export function reportsSource(
  org: string,
  hosted: HostedOperatorsRead,
): ReportsSource {
  if (hosted.status === "unavailable") return hosted;
  const operator = hostedFor(hosted.data, "operator:read");
  const record =
    operator === null
      ? undefined
      : hosted.operators.find((o) => o.grn === operator.grn);
  return {
    status: "ready",
    org,
    hosted:
      operator === null || record === undefined
        ? null
        : { operator, orgGrn: record.orgGrn },
  };
}
