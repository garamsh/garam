import type { Metadata } from "next";

import { CreateOrgForm } from "./_components/create-org-form";

export const metadata: Metadata = { title: "New organization — garam" };

export default function NewOrgPage() {
  return (
    <section className="flex flex-col gap-6">
      <div className="flex flex-col gap-1">
        <h1 className="font-heading text-2xl font-semibold">
          New organization
        </h1>
        <p className="text-sm text-muted-foreground">
          You become the organization&apos;s first owner.
        </p>
      </div>
      <CreateOrgForm />
    </section>
  );
}
