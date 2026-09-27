import "server-only";

import { z } from "zod";

import {
  GaramApiError,
  GaramUnreachableError,
  garamFetchAuthenticated,
} from "@/lib/garam";

import { slugFromGrn } from "../grn";
import { PolicyAttachmentParser, PolicyParser } from "./policies";

/**
 * Whether a lifecycle write may go ahead: it may, it waits for the
 * organization-wide confirmation, or reading the scope failed.
 */
export type ScopeCheck =
  | { status: "proceed" }
  | { status: "confirm" }
  | { status: "error"; message: string };

/**
 * Whether a write that makes `policy` evaluate — activating it, or
 * changing the version an active one evaluates — needs the
 * organization-wide confirmation it was not given. A policy with no
 * attachment applies to every subject (`docs/architecture/authz.md`
 * §Policy attachment), so the scope is read here rather than taken
 * from a page that may be stale; a list garam refuses shows no
 * subject and counts as organization-wide.
 */
export async function checkOrgWideScope(
  org: string,
  policy: string,
  { confirmed, onlyIfActive }: { confirmed: boolean; onlyIfActive: boolean },
): Promise<ScopeCheck> {
  if (confirmed) return { status: "proceed" };
  const base = `/orgs/${encodeURIComponent(org)}/policies`;
  try {
    if (onlyIfActive) {
      const policies = await garamFetchAuthenticated(
        base,
        z.array(PolicyParser),
      );
      const current = policies.find(
        (candidate) => slugFromGrn(candidate.grn) === policy,
      );
      if (current !== undefined && current.status !== "active") {
        return { status: "proceed" };
      }
    }
    const attachments = await garamFetchAuthenticated(
      `${base}/${encodeURIComponent(policy)}/attachments`,
      z.array(PolicyAttachmentParser),
    );
    return attachments.length > 0
      ? { status: "proceed" }
      : { status: "confirm" };
  } catch (error) {
    if (error instanceof GaramApiError && error.status === 403) {
      return { status: "confirm" };
    }
    if (
      error instanceof GaramApiError ||
      error instanceof GaramUnreachableError
    ) {
      return { status: "error", message: error.message };
    }
    throw error;
  }
}
