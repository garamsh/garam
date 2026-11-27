import type { Metadata } from "next";

import {
  API_UNREACHABLE_TITLE,
  ApiUnreachableReason,
} from "@garam/design/components/api-unreachable";
import { Banner } from "@garam/design/components/banner";
import { DescriptionList } from "@/components/description-list";
import { Time } from "@/components/time";
import {
  getApp,
  getInstallation,
  getOrg,
  lookupInstanceContact,
  lookupInstances,
  lookupOrgMembers,
  lookupProjects,
  slugFromGrn,
} from "@/features/orgs";

import { ItemCrumb } from "../../../../_components/item-crumb";
import { orgHref } from "../../../../_components/navigation";
import { PageHeader } from "../../../../_components/page-header";
import { RefusedRead } from "../../../../_components/refused-read";
import { DetailPage } from "../../../../_components/templates/detail-page";
import {
  YourAccess,
  accessDecisions,
  readAccess,
} from "../../../../_components/your-access";
import { INSTANCES_LABEL, VISIBILITY_LABEL } from "../_components/app-labels";
import { CreateInstanceDialog } from "./_components/create-instance-dialog";
import { InstanceTable, type InstanceRow } from "./_components/instance-table";
import { UninstallForm } from "./_components/uninstall-form";

/** Head metadata for an installation's route; Next.js reads it by name. */
export const metadata: Metadata = { title: "App — garam" };

type Params = {
  params: Promise<{ org: string; app: string }>;
  searchParams: Promise<{ tab?: string }>;
};

/** What the page says where garam did not show the installation. */
function Unavailable({ children }: { children: React.ReactNode }) {
  return (
    <>
      <PageHeader title="App" />
      {children}
    </>
  );
}

/**
 * One installed app (ADR-0192): what it is, the scope it was installed
 * over, its instances with when each last made contact, and
 * uninstalling it.
 *
 * An instance's last contact is read once for each, beside the page's
 * own reads; one that could not be read says so rather than "never".
 * The page asks the server once which actions the caller may take on
 * this installation and leaves out only what it is told is denied
 * (ADR-0103); creating an instance is left out as well where the app
 * runs none, or runs one and it exists.
 */
export default async function InstallationPage({
  params,
  searchParams,
}: Params) {
  const [{ org, app }, { tab }] = await Promise.all([params, searchParams]);
  // Started before the page's own reads, so it runs beside them.
  const access = readAccess(org, [
    {
      key: "register",
      label: "Create an instance",
      check: {
        action: "iam:instance.register",
        resource: { type: "installation", id: app },
      },
    },
    {
      key: "uninstall",
      label: "Uninstall it",
      check: {
        action: "iam:installation.delete",
        resource: { type: "installation", id: app },
      },
    },
  ]);
  const [
    organization,
    installation,
    record,
    instances,
    projects,
    members,
    readings,
  ] = await Promise.all([
    getOrg(org),
    getInstallation(org, app),
    getApp(org, app),
    lookupInstances(org, app),
    lookupProjects(org),
    lookupOrgMembers(org),
    access,
  ]);
  const permissions = accessDecisions(readings);

  if (
    organization.status === "unreachable" ||
    installation.status === "unreachable" ||
    record.status === "unreachable"
  ) {
    return (
      <Unavailable>
        <Banner tone="warn" title={API_UNREACHABLE_TITLE}>
          <ApiUnreachableReason subject="this app" />
        </Banner>
      </Unavailable>
    );
  }
  if (installation.status === "forbidden" || record.status === "forbidden") {
    return (
      <Unavailable>
        <RefusedRead subject="this app" />
      </Unavailable>
    );
  }

  const i = installation.data;
  const a = record.data;
  const allowed = (name: Parameters<typeof permissions.get>[0]) =>
    permissions.get(name) !== "denied";
  const projectNames =
    projects.status === "ok"
      ? new Map(projects.data.map((project) => [project.grn, project.name]))
      : new Map<string, string>();
  const installedBy =
    members.status === "ok"
      ? members.data.find((member) => member.userGrn === i.createdBy)?.nickname
      : undefined;
  const appsHref = orgHref(org, "apps");

  const instanceList = instances.status === "ok" ? instances.data : [];
  const contacts = await Promise.all(
    instanceList.map((instance) =>
      lookupInstanceContact(org, app, slugFromGrn(instance.grn)),
    ),
  );
  const rows: InstanceRow[] = instanceList.map((instance, index) => {
    const contact = contacts[index];
    return {
      identifier: slugFromGrn(instance.grn),
      name: instance.name,
      kind: instance.kind,
      lastContact: contact?.status === "ok" ? contact.data : undefined,
    };
  });
  // `none` admits no instance (404) and `one` admits a second never
  // (409), so the control is offered only where it can succeed.
  const canCreate =
    allowed("register") &&
    (a.instances === "many" || (a.instances === "one" && rows.length === 0));

  const overview = (
    <DescriptionList
      items={[
        {
          term: "Description",
          description: a.description || (
            <span className="text-muted-foreground">None</span>
          ),
        },
        {
          term: "Scope",
          description:
            i.projects.length === 0 ? (
              "The whole organization"
            ) : (
              <ul className="flex flex-col gap-0.5">
                {i.projects.map((grn) => (
                  <li key={grn}>{projectNames.get(grn) ?? slugFromGrn(grn)}</li>
                ))}
              </ul>
            ),
        },
        { term: "Visibility", description: VISIBILITY_LABEL[a.visibility] },
        { term: "Instances", description: INSTANCES_LABEL[a.instances] },
        {
          term: "Actions it requests",
          description:
            a.requestedActions.length === 0 ? (
              <span className="text-muted-foreground">None</span>
            ) : (
              <ul className="flex flex-col gap-0.5">
                {a.requestedActions.map((action) => (
                  <li key={action} className="font-mono text-xs">
                    {action}
                  </li>
                ))}
              </ul>
            ),
        },
        {
          term: "Installed",
          description: (
            <span>
              <Time value={i.createdAt} />
              {installedBy && (
                <span className="text-muted-foreground"> by {installedBy}</span>
              )}
            </span>
          ),
        },
      ]}
    />
  );

  const instancesTab =
    instances.status === "ok" ? (
      <div className="flex flex-col gap-4">
        {canCreate && (
          <div className="flex justify-end">
            <CreateInstanceDialog org={org} installation={app} />
          </div>
        )}
        {a.instances === "none" && (
          <p className="text-muted-foreground">
            This app runs no instance, so there is nothing to create.
          </p>
        )}
        <InstanceTable instancesHref={`${appsHref}/${app}`} instances={rows} />
      </div>
    ) : instances.status === "forbidden" ? (
      <RefusedRead subject="this app's instances" />
    ) : (
      <Banner tone="warn" title={API_UNREACHABLE_TITLE}>
        <ApiUnreachableReason subject="this app's instances" />
      </Banner>
    );

  return (
    <>
      <ItemCrumb label={a.name} />
      <DetailPage
        name={a.name}
        idLabel="Identifier"
        id={app}
        defaultTab={tab}
        tabs={[
          { value: "overview", label: "Overview", content: overview },
          { value: "instances", label: "Instances", content: instancesTab },
        ]}
        dangerZone={
          allowed("uninstall")
            ? [
                {
                  title: "Uninstall this app",
                  control: (
                    <UninstallForm org={org} installation={app} name={a.name} />
                  ),
                },
              ]
            : undefined
        }
      />
      <YourAccess readings={readings} />
    </>
  );
}
