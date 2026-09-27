import { z } from "zod";

import { garamFetchAuthenticated, parserFor } from "@/lib/garam";
import type { components } from "@/types/api";

import { parseGrn } from "../grn";
import { orgPath, refusableRead, type RefusableRead } from "./orgs";

/**
 * A starting point an organization keeps for defining agents: a name,
 * what it is for, and values a new definition may copy once. It names
 * no operator and no agent, and nothing links a definition back to the
 * template it was started from (ADR-0075).
 */
export type AgentTemplate = components["schemas"]["AgentTemplate"];

const GrnParser = z.string().refine((value) => parseGrn(value) !== null);

/** Parses a template as `api/openapi.yaml` describes one. */
export const AgentTemplateParser = parserFor<AgentTemplate>()(
  z.object({
    grn: GrnParser,
    orgGrn: GrnParser,
    name: z.string(),
    description: z.string(),
    values: z.record(z.string(), z.string()),
    createdBy: GrnParser,
    createdAt: z.string(),
    updatedAt: z.string(),
  }),
);

/**
 * The organization's templates, ordered by name. A refusal is returned
 * rather than folded into not-found: the pages reading this also read
 * the organization, whose refusal stays not-found (ADR-0011).
 */
export function listAgentTemplates(
  org: string,
): Promise<RefusableRead<AgentTemplate[]>> {
  return refusableRead(
    garamFetchAuthenticated(
      `/orgs/${orgPath(org)}/agent-templates`,
      z.array(AgentTemplateParser),
    ),
  );
}

/** One template of the organization, by its identifier. */
export function getAgentTemplate(
  org: string,
  template: string,
): Promise<RefusableRead<AgentTemplate>> {
  return refusableRead(
    garamFetchAuthenticated(
      `/orgs/${orgPath(org)}/agent-templates/${orgPath(template)}`,
      AgentTemplateParser,
    ),
  );
}
