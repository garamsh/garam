import type { Metadata } from "next";

import { PageHeader } from "../../_components/page-header";
import { CreateOrgForm } from "./_components/create-org-form";

/** Head metadata for the create route; Next.js reads it by name. */
export const metadata: Metadata = { title: "New organization — garam" };

/** Hosts the create form; the work happens in the Server Action. */
export default function NewOrgPage() {
  return (
    <>
      <PageHeader
        title="New organization"
        description="You become the organization's first owner."
      />
      <CreateOrgForm />
    </>
  );
}
