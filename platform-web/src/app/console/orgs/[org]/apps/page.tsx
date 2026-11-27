import type { Metadata } from "next";

import {
  API_UNREACHABLE_TITLE,
  ApiUnreachableReason,
} from "@garam/design/components/api-unreachable";
import { Banner } from "@garam/design/components/banner";
import {
  lookupApps,
  lookupInstallations,
  lookupProjects,
  slugFromGrn,
} from "@/features/orgs";
import { checkPermissions } from "@/lib/permission-check";

import { orgHref } from "../../../_components/navigation";
import { RefusedRead } from "../../../_components/refused-read";
import { ListPage } from "../../../_components/templates/list-page";
import { AppTable } from "./_components/app-table";

/** Head metadata for the apps route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Apps — garam" };

type Params = { params: Promise<{ org: string }> };

/**
 * The apps the organization may install and which of them it has
 * (ADR-0192): its own and every public one, each installed once. An
 * installed app opens the page of its installation; an app not yet
 * installed opens a dialog saying what installing writes, which only
 * an owner may do (`iam:installation.create`). The page leaves the
 * install out only where the server answers that denial, so a check
 * that could not be asked still offers it and the API refuses.
 */
export default async function AppsPage({ params }: Params) {
  const { org } = await params;
  const [apps, installations, projects, permissions] = await Promise.all([
    lookupApps(org),
    lookupInstallations(org),
    lookupProjects(org),
    checkPermissions(org, { install: { action: "iam:installation.create" } }),
  ]);

  const installed = new Set(
    installations.status === "ok"
      ? installations.data.map((installation) =>
          slugFromGrn(installation.appGrn),
        )
      : [],
  );

  return (
    <ListPage
      title="Apps"
      description="Systems that connect to this organization. Installing one gives it the actions it requests."
      count={apps.status === "ok" ? apps.data.length : undefined}
    >
      {apps.status === "unreachable" ||
      installations.status === "unreachable" ? (
        <Banner tone="warn" title={API_UNREACHABLE_TITLE}>
          <ApiUnreachableReason subject="this organization's apps" />
        </Banner>
      ) : apps.status === "forbidden" ? (
        <RefusedRead subject="this organization's apps" />
      ) : (
        <AppTable
          org={org}
          appsHref={orgHref(org, "apps")}
          canInstall={permissions.get("install") !== "denied"}
          projects={
            projects.status === "ok"
              ? projects.data.map(({ grn, name }) => ({ grn, name }))
              : []
          }
          apps={apps.data.map((app) => ({
            ...app,
            installed: installed.has(app.slug),
          }))}
        />
      )}
    </ListPage>
  );
}
