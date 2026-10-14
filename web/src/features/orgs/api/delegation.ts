import "server-only";

import { z } from "zod";

import {
  GaramApiError,
  GaramContractError,
  GaramUnreachableError,
  garamFetchAuthenticated,
  parserFor,
} from "@/lib/garam";
import {
  CONTRACT_VERSION,
  CONTRACT_VERSION_HEADER,
} from "@/lib/operation-authority";
import type { components } from "@/types/api";

import { slugFromGrn } from "@/lib/grn";
import { GrnParser, OperatorParser, orgPath, type Operator } from "./orgs";

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
        "agent:cutover",
        "operator:read",
      ]),
    ),
    expiresAt: z.string(),
    grantedBy: GrnParser,
    grantedAt: z.string(),
  }),
);

/**
 * What reading a hosted operator's grant answered: the grant, the fact
 * that it holds none, a refusal, or no usable answer — the backend
 * unreachable, failing, or answering outside its specification.
 */
export type DelegationRead =
  | { status: "ok"; data: Delegation }
  | { status: "none" }
  | { status: "forbidden" }
  | { status: "unreachable" };

/**
 * Whether error is garam's answer — a refusal, an outage or a body
 * outside its specification — rather than a login redirect or a bug,
 * which travel on.
 */
function isGaramAnswer(
  error: unknown,
): error is GaramApiError | GaramUnreachableError | GaramContractError {
  return (
    error instanceof GaramApiError ||
    error instanceof GaramUnreachableError ||
    error instanceof GaramContractError
  );
}

/**
 * The hosted operator's current grant. It never throws for an answer
 * garam gave: a page that shows a grant is never failed by reading one.
 */
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
    if (!isGaramAnswer(error)) throw error;
    if (error instanceof GaramApiError && error.status === 404) {
      return { status: "none" };
    }
    if (error instanceof GaramApiError && error.status === 403) {
      return { status: "forbidden" };
    }
    return { status: "unreachable" };
  }
}

/**
 * An operator with a registered control endpoint: the audience the
 * console hands operations to, where it sends them, and the grant they
 * are minted under. Where the grant could not be read, `delegation` is
 * null and `problem` says why.
 */
export type HostedOperator = {
  grn: string;
  identifier: string;
  name: string;
  endpoint: string;
  delegation: Delegation | null;
  problem: string | null;
};

/**
 * What the console can reach of the organization's control services:
 * every operator with a registered endpoint and its grant, or why none
 * could be read. A control service panel shows the reason and stays
 * disabled; nothing else on its page waits on this read.
 */
export type HostedOperatorsRead =
  | { status: "ok"; data: HostedOperator[]; operators: Operator[] }
  | { status: "unavailable"; reason: string };

/**
 * The organization's hosted operators. It never fails the page it is
 * read for: a refusal or an error becomes a reason the control service
 * panels show (#1170).
 */
export async function listHostedOperators(
  org: string,
): Promise<HostedOperatorsRead> {
  let operators: Operator[];
  try {
    operators = await garamFetchAuthenticated(
      `/orgs/${orgPath(org)}/operators`,
      z.array(OperatorParser),
    );
  } catch (error) {
    if (!isGaramAnswer(error)) throw error;
    return {
      status: "unavailable",
      reason:
        error instanceof GaramApiError && error.status === 403
          ? "garam refused to list this organization's operators to you, so no control service is reachable from here."
          : "garam did not answer for this organization's operators, so no control service is reachable from here.",
    };
  }
  const hosted = operators.filter(
    (o): o is Operator & { controlEndpoint: string } =>
      o.controlEndpoint !== undefined,
  );
  const delegations = await Promise.all(
    hosted.map((o) => getOperatorDelegation(org, slugFromGrn(o.grn))),
  );
  return {
    status: "ok",
    operators,
    data: hosted.map((o, i) => {
      const delegation = delegations[i] ?? { status: "unreachable" };
      return {
        grn: o.grn,
        identifier: slugFromGrn(o.grn),
        name: o.name,
        endpoint: o.controlEndpoint,
        delegation: delegation.status === "ok" ? delegation.data : null,
        problem:
          delegation.status === "forbidden"
            ? `garam refused to show ${o.name}'s delegation to you.`
            : delegation.status === "unreachable"
              ? `garam did not answer for ${o.name}'s delegation.`
              : null,
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
        (controller === undefined ||
          h.delegation.controllers.includes(controller)),
    ) ??
    hosted[0] ??
    null
  );
}
