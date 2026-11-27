"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import { z } from "zod";

import { garamFetchAuthenticated, refusal } from "@/lib/garam";
import type { components } from "@garam/platform-sdk-ts/api";

import { InstallationParser } from "./apps";

/**
 * What the install form has to show: nothing yet, or a refusal. An
 * installed app is the page it is sent on to.
 */
export type InstallAppState =
  { status: "idle" } | { status: "error"; message: string };

const Schema = z.object({
  org: z.string().min(1),
  app: z.string().trim().min(1),
  scope: z.enum(["organization", "projects"]),
  projects: z.array(z.string().min(1)),
});

/**
 * Installs an app over the whole organization or the projects chosen
 * (ADR-0192 decision 4). garam writes one `permit` per requested
 * action, as the owner who asked, so the form shows that set before
 * this runs; it sends the app's slug and the scope and nothing else.
 */
export async function installApp(
  _state: InstallAppState,
  formData: FormData,
): Promise<InstallAppState> {
  const parsed = Schema.safeParse({
    org: formData.get("org"),
    app: formData.get("app"),
    scope: formData.get("scope"),
    projects: formData.getAll("projects"),
  });
  if (!parsed.success) {
    return { status: "error", message: "The form did not arrive complete." };
  }

  const { org, app, scope, projects } = parsed.data;
  if (scope === "projects" && projects.length === 0) {
    return { status: "error", message: "Choose at least one project." };
  }

  try {
    await garamFetchAuthenticated(
      `/orgs/${encodeURIComponent(org)}/installations`,
      InstallationParser,
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          app,
          projects: scope === "projects" ? projects : undefined,
        } satisfies components["schemas"]["InstallationCreation"]),
      },
    );
  } catch (error) {
    return refusal(error);
  }

  revalidatePath(`/console/orgs/${org}/apps`, "layout");
  redirect(`/console/orgs/${org}/apps/${encodeURIComponent(app)}`);
}
