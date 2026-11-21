import { z } from "zod";

import { garamFetchAuthenticated, parserFor } from "@/lib/garam";
import type { components } from "@garam/platform-sdk-ts/api";

import { orgPath, refusableRead, type RefusableRead } from "./orgs";
import { viaPolicyService } from "./policies";

/**
 * One record of the policy service's audit, read through garam's relay
 * (ADR-0176): a policy write, the denials of one request collapsed
 * with a count, or the denials of a day that overflowed. It holds no
 * policy text, no context and no token.
 */
export type AuditRecord = components["schemas"]["AuditRecord"];
/** What an audit record is. */
export type AuditRecordKind = components["schemas"]["AuditRecordKind"];
/** A page of records, newest first, with the token reading the next. */
export type AuditRecordPage = components["schemas"]["AuditRecordPage"];

const AuditRecordParser = parserFor<AuditRecord>()(
  z.object({
    id: z.number().int(),
    kind: z.enum(["write", "denial", "denial-overflow"]),
    firstAt: z.string(),
    lastAt: z.string(),
    count: z.number().int().min(1),
    service: z.string(),
    subject: z.string(),
    action: z.string(),
    resource: z.string(),
    outcome: z.string(),
    policyIds: z.array(z.string()),
    versionFrom: z.number().int(),
    versionTo: z.number().int(),
    day: z.string().optional(),
  }),
);

const AuditRecordPageParser = parserFor<AuditRecordPage>()(
  z.object({
    records: z.array(AuditRecordParser),
    nextPageToken: z.string().min(1).optional(),
  }),
);

/**
 * An organization's audit records, a page at a time, newest first.
 * Reading them is the policy service's decision on `iam:audit.read`, so
 * a refusal is a state of its own, and a page reading this also reads
 * the organization, whose refusal stays not-found (ADR-0011).
 * `pageToken` is the previous page's `nextPageToken`.
 */
export function listAuditRecords(
  org: string,
  pageToken?: string,
): Promise<RefusableRead<AuditRecordPage>> {
  const query = pageToken ? `?pageToken=${encodeURIComponent(pageToken)}` : "";
  return refusableRead(
    viaPolicyService(
      garamFetchAuthenticated(
        `/orgs/${orgPath(org)}/audit-records${query}`,
        AuditRecordPageParser,
      ),
    ),
  );
}
