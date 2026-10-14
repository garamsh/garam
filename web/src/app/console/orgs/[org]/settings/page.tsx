import type { Metadata } from "next";

import {
  API_UNREACHABLE_TITLE,
  ApiUnreachableReason,
} from "@/components/api-unreachable";
import { Banner } from "@/components/banner";
import { CopyId } from "@/components/copy-id";
import { DescriptionList } from "@/components/description-list";
import { getOrg, getOrgDeletionPreview } from "@/features/orgs";
import { checkPermissions } from "@/lib/permission-check";

import { PageHeader } from "../../../_components/page-header";
import { DangerZone } from "../../../_components/templates/danger-zone";
import { SettingsPage } from "../../../_components/templates/settings-page";
import {
  DeleteOrgDialog,
  type DeletionPreviewRead,
} from "./_components/delete-org-dialog";
import { RenameOrgForm } from "./_components/rename-org-form";

/** Head metadata for the settings route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Settings — garam" };

type Params = { params: Promise<{ org: string }> };

const TITLE = "Settings";
const DESCRIPTION = "What this organization is called, and deleting it.";

/**
 * The organization's own settings: its name, and a danger zone that
 * deletes it. The page asks the server once whether the caller may
 * rename and delete, and leaves a control out only on an explicit
 * deny; an unanswered check renders it and the API refuses (ADR-0103).
 *
 * What a deletion takes is read beside them, so the dialog can state
 * it before the act (ADR-0065).
 */
export default async function OrgSettingsPage({ params }: Params) {
  const { org } = await params;
  const [organization, permissions, preview] = await Promise.all([
    getOrg(org),
    checkPermissions(org, {
      rename: { action: "org:update" },
      delete: { action: "org:delete" },
    }),
    getOrgDeletionPreview(org),
  ]);

  if (organization.status === "unreachable") {
    return (
      <>
        <PageHeader title={TITLE} description={DESCRIPTION} />
        <Banner tone="warn" title={API_UNREACHABLE_TITLE}>
          <ApiUnreachableReason subject="this organization's settings" />
        </Banner>
      </>
    );
  }

  const { name } = organization.data;
  const read: DeletionPreviewRead =
    preview.status === "ok"
      ? { status: "ok", preview: preview.data }
      : { status: preview.status };

  return (
    <SettingsPage
      title={TITLE}
      description={DESCRIPTION}
      sections={[
        {
          id: "general",
          title: "General",
          description:
            "The name is what people see. The identifier names the organization in addresses and does not change.",
          content: (
            <div className="flex flex-col gap-4">
              <DescriptionList
                items={[
                  {
                    term: "Identifier",
                    description: <CopyId value={org} label={name} />,
                  },
                  ...(permissions.get("rename") === "denied"
                    ? [{ term: "Name", description: name }]
                    : []),
                ]}
              />
              {permissions.get("rename") === "denied" ? (
                <p className="text-muted-foreground">
                  The server does not let you rename this organization. An owner
                  or admin of it can.
                </p>
              ) : (
                <RenameOrgForm org={org} name={name} />
              )}
            </div>
          ),
        },
      ]}
      after={
        permissions.get("delete") === "denied" ? undefined : (
          <DangerZone
            actions={[
              {
                title: "Delete this organization",
                description:
                  "Everything it holds goes with it, and nothing brings it back.",
                control: <DeleteOrgDialog org={org} name={name} read={read} />,
              },
            ]}
          />
        )
      }
    />
  );
}
