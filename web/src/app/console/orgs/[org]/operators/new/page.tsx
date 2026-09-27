import type { Metadata } from "next";

import { PageHeader } from "../../../../_components/page-header";
import { RegisterOperatorForm } from "./_components/register-operator-form";

/** Head metadata for the register-operator route; Next.js reads it by name. */
export const metadata: Metadata = { title: "New operator — garam" };

type Params = { params: Promise<{ org: string }> };

/**
 * Registers an operator. The page stays put after registering rather
 * than opening the operator, because the enrollment token is shown
 * here once and nowhere afterwards (ADR-0064).
 */
export default async function NewOperatorPage({ params }: Params) {
  const { org } = await params;

  return (
    <>
      <PageHeader
        title="New operator"
        description="Register an operator and hand it the one-time token it enrols with."
      />
      <RegisterOperatorForm org={org} />
    </>
  );
}
