"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { garamFetchAuthenticated, refusal } from "@/lib/garam";

import { InstallationParser } from "./apps";

/**
 * What the bot-hosting control has to show: nothing yet, a refusal, or
 * the installation as it now stands.
 */
export type InstallationBotHostingState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | { status: "granted" }
  | { status: "withdrawn" };

const Schema = z.object({
  org: z.string().min(1),
  installation: z.string().trim().min(1),
});

function parse(formData: FormData) {
  return Schema.safeParse({
    org: formData.get("org"),
    installation: formData.get("installation"),
  });
}

const NAME_THE_APP: InstallationBotHostingState = {
  status: "error",
  message: "Name the app.",
};

/**
 * Grants the installation bot hosting, as the owner (ADR-0192 decision
 * 10). It is its own act, apart from installing; garam refuses it for an
 * app that does not ask to host bots.
 */
export async function grantInstallationBotHosting(
  _state: InstallationBotHostingState,
  formData: FormData,
): Promise<InstallationBotHostingState> {
  const parsed = parse(formData);
  if (!parsed.success) return NAME_THE_APP;

  const { org, installation } = parsed.data;
  try {
    await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/installations/${encodeURIComponent(installation)}/bot-hosting`,
      InstallationParser,
      { method: "PUT" },
    );
  } catch (error) {
    return refusal(error);
  }

  revalidatePath(`/console/orgs/${org}/apps/${installation}`, "layout");
  return { status: "granted" };
}

/**
 * Withdraws the installation's bot hosting: garam revokes the bot
 * tokens minted for its instances, and the declarations placed on them
 * stay until the organization tombstones them (ADR-0192 decision 10).
 */
export async function withdrawInstallationBotHosting(
  _state: InstallationBotHostingState,
  formData: FormData,
): Promise<InstallationBotHostingState> {
  const parsed = parse(formData);
  if (!parsed.success) return NAME_THE_APP;

  const { org, installation } = parsed.data;
  try {
    await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/installations/${encodeURIComponent(installation)}/bot-hosting`,
      z.unknown(),
      { method: "DELETE" },
    );
  } catch (error) {
    return refusal(error);
  }

  revalidatePath(`/console/orgs/${org}/apps/${installation}`, "layout");
  return { status: "withdrawn" };
}
