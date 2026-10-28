"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { garamFetchAuthenticated, refusal } from "@/lib/garam";
import {
  CONTRACT_VERSION,
  CONTRACT_VERSION_HEADER,
} from "@/lib/operation-authority";
import type { components } from "@garam/platform-client/api";

import { OperatorParser, orgPath } from "./orgs";

/** What the endpoint form has to show. */
export type ControlEndpointState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | { status: "saved"; endpoint: string | null };

const Schema = z.object({
  org: z.string().min(1),
  operator: z.string().min(1),
  intent: z.enum(["set", "clear"]),
  url: z.string(),
});

/**
 * Registers or clears where the operator's control service answers the
 * console (#1170). It is the delegating act, decided on
 * `operator:delegate`, and is a separate write from the operator's
 * name and description: renaming one never moves where operation
 * authority is sent.
 */
export async function saveControlEndpoint(
  _state: ControlEndpointState,
  formData: FormData,
): Promise<ControlEndpointState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    operator: formData.get("operator"),
    intent: formData.get("intent"),
    url: formData.get("url") ?? "",
  });
  if (!parsed.success) {
    return { status: "error", message: "The form did not arrive complete." };
  }
  const { org, operator, intent, url } = parsed.data;
  let saved: string | undefined;
  try {
    ({ controlEndpoint: saved } = await garamFetchAuthenticated(
      `/orgs/${orgPath(org)}/operators/${orgPath(operator)}/control-endpoint`,
      OperatorParser,
      intent === "set"
        ? {
            method: "PUT",
            headers: {
              "content-type": "application/json",
              [CONTRACT_VERSION_HEADER]: CONTRACT_VERSION,
            },
            body: JSON.stringify({
              url: url.trim(),
            } satisfies components["schemas"]["ControlEndpointWrite"]),
          }
        : {
            method: "DELETE",
            headers: { [CONTRACT_VERSION_HEADER]: CONTRACT_VERSION },
          },
    ));
  } catch (error) {
    return refusal(error);
  }
  revalidatePath(`/console/orgs/${org}`, "layout");
  return { status: "saved", endpoint: saved ?? null };
}
