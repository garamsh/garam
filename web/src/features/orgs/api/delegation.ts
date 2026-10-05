import "server-only";

import { z } from "zod";

import {
  GaramApiError,
  GaramUnreachableError,
  garamFetchAuthenticated,
  parserFor,
} from "@/lib/garam";
import { CONTRACT_VERSION, CONTRACT_VERSION_HEADER } from "@/lib/operation-authority";
import type { components } from "@/types/api";

import { slugFromGrn } from "../grn";
import { GrnParser, listOperators, orgPath, type Operator } from "./orgs";

/**
 * A hosted operator's current grant, as the API returns one: which
 * controllers and operations an organization delegated to it, and until
 * when (ADR-0078). It may have passed its expiry; an ended one is never
 * answered.
 */
export type Delegation = components["schemas"]["Delegation"];
/** One operation a grant can carry. */
export type DelegatedOperation = components["schemas"]["DelegatedOperation"];

export const DelegationParser = parserFor<Delegation>()(
  z.object({
    grantId: z.string(),
    operatorGrn: GrnParser,
    controllers: z.array(GrnParser),
    operations: z.array(
      z.enum([
        "agent:create",
        "agent:configure",
        "agent:activate",
        "agent:recover",
        "agent-template:read",
        "agent-template:publish",
        "execution-profile:read",
        "agent:execution-read",
      ]),
    ),
    expiresAt: z.string(),
    grantedBy: GrnParser,
    grantedAt: z.string(),
  }),
);

/**
 * What reading a hosted operator's grant answered: the grant, the fact
 * that it holds none, a refusal, or no answer at all.
 */
export type DelegationRead =
  | { status: "ok"; data: Delegation }
  | { status: "none" }
  | { status: "forbidden" }
  | { status: "unreachable" };

/** The hosted operator's current grant. */
export async function getOperatorDelegation(
  org: string,
  operator: string,
): Promise<DelegationRead> {
  try {
    return {
      status: "ok",
      data: await garamFetchAuthenticated(
        `/orgs/${orgPath(org)}/operators/${orgPath(operator)}/delegation`,
        DelegationParser,
        { headers: { [CONTRACT_VERSION_HEADER]: CONTRACT_VERSION } },
      ),
    };
  } catch (error) {
    if (error instanceof GaramUnreachableError) return { status: "unreachable" };
    if (error instanceof GaramApiError && error.status === 404) {
      return { status: "none" };
    }
    if (error instanceof GaramApiError && error.status === 403) {
      return { status: "forbidden" };
    }
    throw error;
  }
}

/**
 * An operator with a registered control endpoint: the audience the
 * console hands operations to, where it sends them, and the grant they
 * are minted under, null where it holds none.
 */
export type HostedOperator = {
  grn: string;
  identifier: string;
  name: string;
  endpoint: string;
  delegation: Delegation | null;
};

/**
 * What the console can reach of the organization's control services:
 * every operator with a registered endpoint and its grant, or the fact
 * that garam did not answer. A grant garam refused or did not answer is
 * read as none, so its operations stay disabled.
 */
export type HostedOperatorsRead =
  | { status: "ok"; data: HostedOperator[]; operators: Operator[] }
  | { status: "unreachable" };

/** The organization's hosted operators. */
export async function listHostedOperators(
  org: string,
): Promise<HostedOperatorsRead> {
  const operators = await listOperators(org);
  if (operators.status === "unreachable") return operators;
  const hosted = operators.data.filter(
    (o): o is Operator & { controlEndpoint: string } =>
      o.controlEndpoint !== undefined,
  );
  const delegations = await Promise.all(
    hosted.map((o) => getOperatorDelegation(org, slugFromGrn(o.grn))),
  );
  return {
    status: "ok",
    operators: operators.data,
    data: hosted.map((o, i) => {
      const delegation = delegations[i];
      return {
        grn: o.grn,
        identifier: slugFromGrn(o.grn),
        name: o.name,
        endpoint: o.controlEndpoint,
        delegation: delegation?.status === "ok" ? delegation.data : null,
      };
    }),
  };
}

/**
 * The hosted operator a page hands operation to: the first whose grant
 * carries it and, where the operation acts on an agent, lists the
 * controller the agent runs on; otherwise the first hosted operator, so
 * the page can say what its grant lacks; null where none is hosted.
 */
export function hostedFor(
  hosted: readonly HostedOperator[],
  operation: DelegatedOperation,
  controller?: string,
): HostedOperator | null {
  return (
    hosted.find(
      (h) =>
        h.delegation !== null &&
        h.delegation.operations.includes(operation) &&
        (controller === undefined || h.delegation.controllers.includes(controller)),
    ) ??
    hosted[0] ??
    null
  );
}
