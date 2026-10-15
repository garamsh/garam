import { controlReach } from "@/components/control-notice";
import type { HostedOperatorsRead } from "@/features/orgs";

import type { ReportsSource } from "./operator-reports";

/**
 * Where a page reads its operators' reports from: every hosted
 * operator whose grant carries `operator:read`, each with the
 * organization's GRN its record names. A service answers only for the
 * controllers that reached it (garam-agent-operator ADR 0074 §4 at
 * `ba691a6`), so no one of them speaks for the rest.
 *
 * Nothing is asked where every operator is a control service, since
 * none of them is a controller to report on. Where operators are
 * hosted but no grant carries the read, the first one's blocker says
 * why; where none is hosted, nothing is wrong to report.
 */
export function reportsSource(
  org: string,
  hosted: HostedOperatorsRead,
): ReportsSource {
  if (hosted.status === "unavailable") return hosted;
  const identities = hosted.data.map((h) => h.grn);
  const controllers = hosted.operators.filter(
    (o) => !identities.includes(o.grn),
  );
  const services = hosted.data.flatMap((h) => {
    const reach = controlReach(h, "operator:read");
    const record = hosted.operators.find((o) => o.grn === h.grn);
    return reach.status === "ready" && record !== undefined
      ? [{ operator: reach.hosted, orgGrn: record.orgGrn }]
      : [];
  });
  const first = hosted.data[0];
  const reach =
    services.length === 0 && first !== undefined
      ? controlReach(first, "operator:read")
      : null;
  return {
    status: "ready",
    org,
    services: controllers.length === 0 ? [] : services,
    identities,
    blocked:
      controllers.length > 0 && reach?.status === "blocked"
        ? reach.block
        : null,
  };
}
