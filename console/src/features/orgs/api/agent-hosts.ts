import { z } from "zod";

import { garamFetchAuthenticated, parserFor } from "@/lib/garam";
import type { components } from "@garam/platform-client/api";

import { GrnParser, orgPath, refusableRead, type RefusableRead } from "./orgs";

/** A standalone or direct approval, as the API lists one (ADR-0082). */
export type AgentEnrollmentListing =
  components["schemas"]["AgentEnrollmentListing"];
/** A standalone host's supervisor registration, as the API lists one. */
export type AgentSupervisor = components["schemas"]["AgentSupervisor"];

const ReplacementParser = z.object({
  agentGrn: GrnParser,
  assigneeGrn: GrnParser,
  epoch: z.number().int().min(1),
});

const EnrollmentListingParser = parserFor<AgentEnrollmentListing>()(
  z.object({
    id: z.string(),
    placement: z.enum(["standalone", "direct"]),
    state: z.enum(["pending", "spent", "withdrawn", "expired"]),
    agentGrn: GrnParser,
    supervisorGrn: GrnParser,
    hostName: z.string(),
    replaces: ReplacementParser.optional(),
    approvedBy: GrnParser,
    createdAt: z.string(),
    expiresAt: z.string(),
    spentAt: z.string().optional(),
    withdrawnAt: z.string().optional(),
    epoch: z.number().int().min(1).optional(),
  }),
);

const SupervisorParser = parserFor<AgentSupervisor>()(
  z.object({
    grn: GrnParser,
    agentGrn: GrnParser,
    enrollmentId: z.string(),
    placement: z.enum(["standalone", "direct"]),
    state: z.enum(["active", "revoked"]),
    hostName: z.string(),
    epoch: z.number().int().min(1),
    registeredAt: z.string(),
    endedAt: z.string().optional(),
    lastActivatedAt: z.string().optional(),
  }),
);

/** The organization's approvals, oldest first, or why there are none. */
export function lookupAgentEnrollments(
  org: string,
): Promise<RefusableRead<AgentEnrollmentListing[]>> {
  return refusableRead(
    garamFetchAuthenticated(
      `/orgs/${orgPath(org)}/agent-enrollments`,
      z.array(EnrollmentListingParser),
    ),
  );
}

/** The organization's supervisors, oldest first, or why there are none. */
export function lookupAgentSupervisors(
  org: string,
): Promise<RefusableRead<AgentSupervisor[]>> {
  return refusableRead(
    garamFetchAuthenticated(
      `/orgs/${orgPath(org)}/agent-supervisors`,
      z.array(SupervisorParser),
    ),
  );
}
