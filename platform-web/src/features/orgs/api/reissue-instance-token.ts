"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { garamFetchAuthenticated, refusal } from "@/lib/garam";

import { ReissuedRegistrationParser } from "./apps";

/**
 * What the reissue control has to show: nothing yet, a refusal, or the
 * new token garam answered once, carried here for the reason
 * `CreateInstanceState` carries one.
 */
export type ReissueInstanceTokenState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | { status: "reissued"; registrationToken: string; expiresAt: string };

const Schema = z.object({
  org: z.string().min(1),
  installation: z.string().trim().min(1),
  instance: z.string().trim().min(1),
});

/**
 * Answers the instance a new registration token and spends any it held
 * before. The connector that registers with it binds the key it
 * generated, and every key the instance held is withdrawn then: this is
 * how a lost key is recovered (ADR-0184 decision 7). Until then the
 * keys stand.
 */
export async function reissueInstanceToken(
  _state: ReissueInstanceTokenState,
  formData: FormData,
): Promise<ReissueInstanceTokenState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    installation: formData.get("installation"),
    instance: formData.get("instance"),
  });
  if (!parsed.success) {
    return { status: "error", message: "Name the instance to reissue." };
  }

  const { org, installation, instance } = parsed.data;
  try {
    const issued = await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/installations/${encodeURIComponent(installation)}/instances/${encodeURIComponent(instance)}/registration-token`,
      ReissuedRegistrationParser,
      { method: "POST" },
    );
    revalidatePath(
      `/console/orgs/${org}/apps/${installation}/${instance}`,
      "layout",
    );
    return {
      status: "reissued",
      registrationToken: issued.registrationToken,
      expiresAt: issued.expiresAt,
    };
  } catch (error) {
    return refusal(error);
  }
}
