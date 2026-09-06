import type { Metadata } from "next";
import { Server } from "lucide-react";

import { Separator } from "@/components/ui/separator";
import { listOperators } from "@/features/orgs";

import { ApiUnreachable } from "../../../_components/api-unreachable";
import { EmptyState } from "../../../_components/empty-state";
import { PageHeader } from "../../../_components/page-header";
import { OperatorList } from "./_components/operator-list";
import { RegisterOperatorForm } from "./_components/register-operator-form";

/** Head metadata for the operators route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Operators — garam" };

type Params = { params: Promise<{ org: string }> };

/**
 * The operators the organization has registered, and the form that
 * registers one more.
 *
 * Registering is granted by `operator:register` and deleting by
 * `operator:delete`, which owners and admins hold and members do not,
 * while every role reads the list. The form and the delete control are
 * rendered for all of them and the API refuses a caller that holds
 * only the read — the same shape the agents section takes.
 */
export default async function OperatorsPage({ params }: Params) {
  const { org } = await params;
  const operators = await listOperators(org);

  return (
    <>
      <PageHeader
        title="Operators"
        description="The operators this organization has registered. A record is what garam holds — an operator authenticates with the certificate it holds, and registering hands it a one-time token to obtain its first one with."
      />
      {operators.status === "unreachable" ? (
        <ApiUnreachable subject="this organization's operators" />
      ) : operators.data.length === 0 ? (
        <EmptyState
          icon={Server}
          title="No operators registered"
          description="This organization has registered no operators."
        />
      ) : (
        <OperatorList org={org} operators={operators.data} />
      )}
      <Separator />
      <RegisterOperatorForm org={org} />
    </>
  );
}
