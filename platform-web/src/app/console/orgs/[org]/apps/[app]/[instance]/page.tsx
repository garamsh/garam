import type { Metadata } from "next";

import {
  API_UNREACHABLE_TITLE,
  ApiUnreachableReason,
} from "@garam/design/components/api-unreachable";
import { Banner } from "@garam/design/components/banner";
import { DescriptionList } from "@/components/description-list";
import { Time } from "@/components/time";
import {
  getInstallation,
  getInstance,
  getOrg,
  listInstanceKeys,
  lookupInstanceContact,
  lookupOrgMembers,
} from "@/features/orgs";

import { ItemCrumb } from "../../../../../_components/item-crumb";
import { PageHeader } from "../../../../../_components/page-header";
import { RefusedRead } from "../../../../../_components/refused-read";
import { DetailPage } from "../../../../../_components/templates/detail-page";
import {
  YourAccess,
  accessDecisions,
  readAccess,
} from "../../../../../_components/your-access";
import { LastContactCell } from "../_components/instance-table";
import { DeleteInstanceForm } from "./_components/delete-instance-form";
import { KeysPanel } from "./_components/keys-panel";
import { ReissueTokenForm } from "./_components/reissue-token-form";

/** Head metadata for an instance's route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Instance — garam" };

type Params = {
  params: Promise<{ org: string; app: string; instance: string }>;
  searchParams: Promise<{ tab?: string }>;
};

/** What the page says where garam did not show the instance. */
function Unavailable({ children }: { children: React.ReactNode }) {
  return (
    <>
      <PageHeader title="Instance" />
      {children}
    </>
  );
}

/**
 * One instance of an installed app (ADR-0192): what it says it is, when
 * it last made contact, its keys and where each stands, a new
 * registration token for it, and deleting it. Its kind is shown as the
 * text it was stored as.
 *
 * Every action here is decided on the installation (`iam:instance.*`),
 * so the page asks the server once about it and leaves out only what it
 * is told is denied (ADR-0103).
 */
export default async function InstancePage({ params, searchParams }: Params) {
  const [{ org, app, instance }, { tab }] = await Promise.all([
    params,
    searchParams,
  ]);
  const installationResource = { type: "installation", id: app } as const;
  // Started before the page's own reads, so it runs beside them.
  const access = readAccess(org, [
    {
      key: "read",
      label: "Read its report",
      check: { action: "iam:instance.read", resource: installationResource },
    },
    {
      key: "reissue",
      label: "Reissue its registration token",
      check: { action: "iam:instance.reissue", resource: installationResource },
    },
    {
      key: "delete",
      label: "Delete it",
      check: { action: "iam:instance.delete", resource: installationResource },
    },
  ]);
  const [organization, installation, record, keys, contact, members, readings] =
    await Promise.all([
      getOrg(org),
      getInstallation(org, app),
      getInstance(org, app, instance),
      listInstanceKeys(org, app, instance),
      lookupInstanceContact(org, app, instance),
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
          <ApiUnreachableReason subject="this instance" />
        </Banner>
      </Unavailable>
    );
  }
  if (installation.status === "forbidden" || record.status === "forbidden") {
    return (
      <Unavailable>
        <RefusedRead subject="this instance" />
      </Unavailable>
    );
  }

  const r = record.data;
  const allowed = (name: Parameters<typeof permissions.get>[0]) =>
    permissions.get(name) !== "denied";
  const createdBy =
    members.status === "ok"
      ? members.data.find((member) => member.userGrn === r.createdBy)?.nickname
      : undefined;

  const overview = (
    <DescriptionList
      items={[
        {
          term: "Description",
          description: r.description || (
            <span className="text-muted-foreground">None</span>
          ),
        },
        {
          term: "Kind",
          description: r.kind || (
            <span className="text-muted-foreground">Not said</span>
          ),
        },
        {
          term: "Last contact",
          description: (
            <LastContactCell
              value={contact.status === "ok" ? contact.data : undefined}
            />
          ),
        },
        {
          term: "Created",
          description: (
            <span>
              <Time value={r.createdAt} />
              {createdBy && (
                <span className="text-muted-foreground"> by {createdBy}</span>
              )}
            </span>
          ),
        },
      ]}
    />
  );

  const keysTab =
    keys.status === "ok" ? (
      <div className="flex flex-col gap-4">
        {allowed("reissue") && (
          <div className="flex justify-end">
            <ReissueTokenForm
              org={org}
              installation={app}
              instance={instance}
              name={r.name}
            />
          </div>
        )}
        <KeysPanel
          org={org}
          installation={app}
          instance={instance}
          keys={keys.data}
          canRevoke={allowed("reissue")}
        />
      </div>
    ) : keys.status === "forbidden" ? (
      <RefusedRead subject="this instance's keys" />
    ) : (
      <Banner tone="warn" title={API_UNREACHABLE_TITLE}>
        <ApiUnreachableReason subject="this instance's keys" />
      </Banner>
    );

  return (
    <>
      <ItemCrumb label={r.name} />
      <DetailPage
        name={r.name}
        idLabel="Identifier"
        id={instance}
        defaultTab={tab}
        tabs={[
          { value: "overview", label: "Overview", content: overview },
          { value: "keys", label: "Keys", content: keysTab },
        ]}
        dangerZone={
          allowed("delete")
            ? [
                {
                  title: "Delete this instance",
                  control: (
                    <DeleteInstanceForm
                      org={org}
                      installation={app}
                      instance={instance}
                      name={r.name}
                    />
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
