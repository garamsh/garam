import type { Metadata } from "next";
import { Server } from "lucide-react";

import { Separator } from "@/components/ui/separator";
import { listOperators } from "@/features/orgs";
import { checkPermissions } from "@/lib/permission-check";

import { ApiUnreachable } from "../../../_components/api-unreachable";
import { EmptyState } from "../../../_components/empty-state";
import { PageHeader } from "../../../_components/page-header";
import { OperatorList } from "./_components/operator-list";
import { RegisterOperatorForm } from "./_components/register-operator-form";

/** Head metadata for the operators route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Operators — garam" };

type Params = { params: Promise<{ org: string }> };

/**
 * The operators the organization has registered, each opening a page
 * of its own, and the form that registers one more (ADR-0074). The form
 * is here rather than on a create route because an identifier may be
 * `new`, and that route would take the operator's page.
 *
 * Every role reads the list, and registering is `operator:register`,
 * which owners and admins hold and members do not. The page asks the
 * server whether the caller may register and leaves the form out when
 * the answer is no. A check that could not be asked renders the form,
 * and the API refuses whoever holds only the read.
 */
export default async function OperatorsPage({ params }: Params) {
  const { org } = await params;
  const [operators, permissions] = await Promise.all([
    listOperators(org),
    checkPermissions(org, { register: { action: "operator:register" } }),
  ]);

  return (
    <>
      <PageHeader
        title="Operators"
        description="Operators this organization has registered."
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
      {permissions.get("register") !== "denied" && (
        <>
          <Separator />
          <RegisterOperatorForm org={org} />
        </>
      )}
    </>
  );
}
