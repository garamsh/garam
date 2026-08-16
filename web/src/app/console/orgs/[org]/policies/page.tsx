import type { Metadata } from "next";
import { ScrollText } from "lucide-react";

import { EmptyState } from "../../../_components/empty-state";
import { PageHeader } from "../../../_components/page-header";

export const metadata: Metadata = { title: "Policies — garam" };

export default function PoliciesPage() {
  return (
    <>
      <PageHeader
        title="Policies"
        description="The policy documents this organization authorizes against."
      />
      <EmptyState
        icon={ScrollText}
        title="Not readable here yet"
        description="Policies are managed over the HTTP API; the console does not read them yet."
      />
    </>
  );
}
