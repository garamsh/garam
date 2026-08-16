import type { Metadata } from "next";

import { PageHeader } from "../../_components/page-header";
import { CreateOrgForm } from "./_components/create-org-form";

export const metadata: Metadata = { title: "New organization — garam" };

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
