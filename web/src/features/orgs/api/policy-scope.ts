import "server-only";

import { z } from "zod";

import { GaramApiError, garamFetchAuthenticated, refusal } from "@/lib/garam";

import { slugFromGrn } from "../grn";
import { PolicyAttachmentParser, PolicyParser } from "./policies";

/**
 * Whether a lifecycle write may go ahead: it may; it waits for the
 * confirmation because the policy has no attachment, or because garam
 * refused to list its attachments and the scope is unknown; or reading
 * the scope failed.
 */
export type ScopeCheck =
  | { status: "proceed" }
  | { status: "confirm-org-wide" }
  | { status: "confirm-unknown-scope" }
  | { status: "error"; message: string };

/**
 * Whether a write that makes `policy` evaluate — activating it, or
 * changing the version an active one evaluates — needs the
 * organization-wide confirmation it was not given. A policy with no
 * attachment applies to every subject (`docs/architecture/authz.md`
 * §Policy attachment), so the scope is read here rather than taken
 * from a page that may be stale. A list garam refuses is not an empty
 * one: its scope is unknown, and it is confirmed as possibly reaching
 * everyone. The read and the write are two requests with no condition
 * joining them, so an attachment removed between the two is not seen.
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
      : { status: "confirm-org-wide" };
  } catch (error) {
    if (error instanceof GaramApiError && error.status === 403) {
      return { status: "confirm-unknown-scope" };
    }
    return refusal(error);
  }
}
