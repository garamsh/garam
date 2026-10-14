import { z } from "zod";

import { garamFetchAuthenticated, parserFor } from "@/lib/garam";
import type { components } from "@/types/api";

import { orgPath, refusableRead, type RefusableRead } from "./orgs";

/** The public record of one key an operator was issued (ADR-0102). */
export type OperatorKey = components["schemas"]["OperatorKey"];

/** A key garam generated, answered once with its private half. */
export type IssuedOperatorKey = components["schemas"]["IssuedOperatorKey"];

export const OperatorKeyParser = parserFor<OperatorKey>()(
  z.object({
    kid: z.string(),
    algorithm: z.enum(["RS256"]),
    state: z.enum(["active", "retiring", "revoked"]),
    issuedAt: z.string(),
    retiresAt: z.string().optional(),
    revokedAt: z.string().optional(),
    lastExchangedAt: z.string().optional(),
  }),
);

export const IssuedOperatorKeyParser = parserFor<IssuedOperatorKey>()(
  z.object({
    key: OperatorKeyParser,
    privateKeyPem: z.string(),
    clientId: z.string(),
    tokenUrl: z.string(),
  }),
);

const OperatorKeyListParser = parserFor<
  components["schemas"]["OperatorKeyList"]
>()(z.object({ keys: z.array(OperatorKeyParser) }));

/**
 * The keys an operator was issued, withdrawn ones included, oldest
 * first. A refusal is returned rather than answered with not-found: the
 * page reading it has read the operator, so a refusal here is about the
 * keys alone.
 */
export async function listOperatorKeys(
  org: string,
  operator: string,
): Promise<RefusableRead<OperatorKey[]>> {
  const read = await refusableRead(
    garamFetchAuthenticated(
      `/orgs/${orgPath(org)}/operators/${encodeURIComponent(operator)}/keys`,
      OperatorKeyListParser,
    ),
  );
  return read.status === "ok" ? { status: "ok", data: read.data.keys } : read;
}
