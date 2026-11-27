import { z } from "zod";

import { garamFetchAuthenticated, parserFor } from "@/lib/garam";
import type { components } from "@garam/platform-sdk-ts/api";

import { GrnParser, orgPath, refusableRead, type RefusableRead } from "./orgs";

/** An app an organization may install, as the API returns one. */
export type App = components["schemas"]["App"];
/** One app installed in the organization, as the API returns one. */
export type Installation = components["schemas"]["Installation"];
/**
 * One running copy of an installation's app. Its kind is what the
 * organization says it runs on, a string garam stores and never reads
 * (ADR-0192 decision 1).
 */
export type Instance = components["schemas"]["Instance"];
/** The public record of one key an instance registered. */
export type InstanceKey = components["schemas"]["InstanceKey"];
/** A one-time registration token, answered once. */
export type InstanceRegistrationToken =
  components["schemas"]["InstanceRegistrationToken"];
/** An instance created, with the token it registers with. */
export type CreatedInstance = components["schemas"]["CreatedInstance"];

export const AppParser = parserFor<App>()(
  z.object({
    grn: GrnParser,
    slug: z.string(),
    name: z.string(),
    description: z.string(),
    visibility: z.enum(["private", "public"]),
    requestedActions: z.array(z.string()),
    requestsBotHosting: z.boolean(),
    instances: z.enum(["none", "one", "many"]),
    delivery: z.enum(["pull"]),
  }),
);

export const InstallationParser = parserFor<Installation>()(
  z.object({
    grn: GrnParser,
    orgGrn: GrnParser,
    appGrn: GrnParser,
    incarnation: z.string(),
    projects: z.array(GrnParser),
    createdBy: GrnParser,
    createdAt: z.string(),
  }),
);

export const InstanceParser = parserFor<Instance>()(
  z.object({
    grn: GrnParser,
    installationGrn: GrnParser,
    name: z.string(),
    description: z.string(),
    kind: z.string(),
    orgGrn: GrnParser,
    createdBy: GrnParser,
    createdAt: z.string(),
  }),
);

const InstanceKeyParser = parserFor<InstanceKey>()(
  z.object({
    kid: z.string(),
    state: z.enum(["active", "retiring", "revoked"]),
    createdAt: z.string(),
    retiresAt: z.string().optional(),
    revokedAt: z.string().optional(),
  }),
);

const InstanceRegistrationTokenParser = parserFor<InstanceRegistrationToken>()(
  z.object({ registrationToken: z.string(), expiresAt: z.string() }),
);

export const CreatedInstanceParser = parserFor<CreatedInstance>()(
  z.object({
    instance: InstanceParser,
    registration: InstanceRegistrationTokenParser,
  }),
);

export const ReissuedRegistrationParser = InstanceRegistrationTokenParser;

/** The installation's path below its organization. */
function installationPath(org: string, installation: string): string {
  return `/orgs/${orgPath(org)}/installations/${orgPath(installation)}`;
}

/**
 * The apps the organization may install: its own and every public one.
 * A refusal is returned rather than answered with not-found, for a
 * page that also reads the organization.
 */
export function lookupApps(org: string): Promise<RefusableRead<App[]>> {
  return refusableRead(
    garamFetchAuthenticated(`/orgs/${orgPath(org)}/apps`, z.array(AppParser)),
  );
}

/** One app the organization may install, by its slug. */
export function getApp(org: string, app: string): Promise<RefusableRead<App>> {
  return refusableRead(
    garamFetchAuthenticated(
      `/orgs/${orgPath(org)}/apps/${orgPath(app)}`,
      AppParser,
    ),
  );
}

/** The apps the organization has installed, ordered by GRN. */
export function lookupInstallations(
  org: string,
): Promise<RefusableRead<Installation[]>> {
  return refusableRead(
    garamFetchAuthenticated(
      `/orgs/${orgPath(org)}/installations`,
      z.array(InstallationParser),
    ),
  );
}

/** One installation, named by its app's slug. */
export function getInstallation(
  org: string,
  installation: string,
): Promise<RefusableRead<Installation>> {
  return refusableRead(
    garamFetchAuthenticated(
      installationPath(org, installation),
      InstallationParser,
    ),
  );
}

/** The instances of an installation, ordered by GRN. */
export function lookupInstances(
  org: string,
  installation: string,
): Promise<RefusableRead<Instance[]>> {
  return refusableRead(
    garamFetchAuthenticated(
      `${installationPath(org, installation)}/instances`,
      z.array(InstanceParser),
    ),
  );
}

/** One instance of an installation, by its identifier. */
export function getInstance(
  org: string,
  installation: string,
  instance: string,
): Promise<RefusableRead<Instance>> {
  return refusableRead(
    garamFetchAuthenticated(
      `${installationPath(org, installation)}/instances/${orgPath(instance)}`,
      InstanceParser,
    ),
  );
}

/**
 * The keys an instance registered, withdrawn ones included, oldest
 * first. A refusal is returned: the page reading it has read the
 * instance, so a refusal here is about the keys alone.
 */
export async function listInstanceKeys(
  org: string,
  installation: string,
  instance: string,
): Promise<RefusableRead<InstanceKey[]>> {
  const read = await refusableRead(
    garamFetchAuthenticated(
      `${installationPath(org, installation)}/instances/${orgPath(instance)}/keys`,
      z.object({ keys: z.array(InstanceKeyParser) }),
    ),
  );
  return read.status === "ok" ? { status: "ok", data: read.data.keys } : read;
}

/**
 * When the instance's latest authenticated request reached garam, null
 * where none has. Only that is read of what the instance reported: its
 * reports are the bot domain's to show (#2082).
 */
export function lookupInstanceContact(
  org: string,
  installation: string,
  instance: string,
): Promise<RefusableRead<string | null>> {
  return refusableRead(
    garamFetchAuthenticated(
      `${installationPath(org, installation)}/instances/${orgPath(instance)}/report`,
      z.object({ lastContactAt: z.string().nullable() }),
    ).then((view) => view.lastContactAt),
  );
}
