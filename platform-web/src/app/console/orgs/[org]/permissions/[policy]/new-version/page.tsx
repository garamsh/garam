import type { Metadata } from "next";
import Link from "next/link";

import { Banner } from "@garam/design/components/banner";
import { Button } from "@garam/design/ui/button";
import { getIamPolicy, getOrg } from "@/features/orgs";
import { checkPermissions } from "@/lib/permission-check";

import { ApiUnreachable } from "../../../../../_components/api-unreachable";
import { ItemCrumb } from "../../../../../_components/item-crumb";
import { orgHref } from "../../../../../_components/navigation";
import { PageHeader } from "../../../../../_components/page-header";
import { RefusedRead } from "../../../../../_components/refused-read";
import { SettingsPage } from "../../../../../_components/templates/settings-page";
import { WRITE_ACTION } from "../../_components/policy-actions";
import { NewVersionForm } from "./_components/new-version-form";

/** Head metadata for the new-version route; Next.js reads it by name. */
export const metadata: Metadata = { title: "New version — garam" };

type Params = { params: Promise<{ org: string; policy: string }> };

/** The page's header where no policy could be shown. */
function Unshown({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="New version" />
      {children}
    </div>
  );
}

/**
 * Writes the next version of a guardrail or a permit (ADR-0173). The
 * form starts from the text the policy decides by. A role link takes
 * no version, since a changed link is a new one, so its page says so
 * in place of a form. The server is asked whether the caller may write
 * the policy, as its own page asks, and only an explicit deny
 * withdraws the form; the policy service enforces the write either way.
 */
export default async function NewVersionPage({ params }: Params) {
  const { org, policy: slug } = await params;
  const [organization, read] = await Promise.all([
    getOrg(org),
    getIamPolicy(org, slug),
  ]);
  if (organization.status === "unreachable" || read.status === "unreachable") {
    return (
      <Unshown>
        <ApiUnreachable subject="this policy" />
      </Unshown>
    );
  }
  if (read.status === "forbidden") {
    return (
      <Unshown>
        <RefusedRead subject="this policy" />
      </Unshown>
    );
  }

  const { policy, versions } = read.data;
  const back = `${orgHref(org, "permissions")}/${policy.id}`;
  const action = WRITE_ACTION[policy.kind].write;
  const permissions = await checkPermissions(org, {
    write: { action, resource: { type: "policy", id: policy.id } },
  });
  const current = versions.find(
    (version) => version.version === policy.defaultVersion,
  )?.body;

  const content =
    policy.kind === "role-link" ? (
      <div className="flex flex-col gap-3">
        <Banner tone="info" title="A role link takes no new version">
          Create a new role link for the change, then delete this one.
        </Banner>
        <div>
          <Button asChild variant="ghost">
            <Link href={back}>Back to the policy</Link>
          </Button>
        </div>
      </div>
    ) : permissions.get("write") === "denied" ? (
      <Banner tone="info" title="No policy lets you change this policy" />
    ) : (
      <NewVersionForm
        org={org}
        policy={policy.id}
        kind={policy.kind}
        startingText={current?.kind === policy.kind ? current.text : ""}
      />
    );

  return (
    <>
      <ItemCrumb label={policy.name} />
      <SettingsPage
        title="New version"
        description={`A version of ${policy.name}. It is stored and not the default, so the policy decides as it did until you move the default.`}
        sections={[{ id: "version", title: "Version", content }]}
      />
    </>
  );
}
