import type { Metadata } from "next";
import Link from "next/link";
import { Server } from "lucide-react";

import { Button } from "@/components/ui/button";
import { listOperators } from "@/features/orgs";

import { ApiUnreachable } from "../../../_components/api-unreachable";
import { EmptyState } from "../../../_components/empty-state";
import { NEW_ITEM, orgHref } from "../../../_components/navigation";
import { PageHeader } from "../../../_components/page-header";
import { OperatorList } from "./_components/operator-list";

/** Head metadata for the operators route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Operators — garam" };

type Params = { params: Promise<{ org: string }> };

/**
 * The operators the organization has registered, each opening a page
 * of its own (ADR-0074).
 *
 * Registering is granted by `operator:register`, which owners and
 * admins hold and members do not, while every role reads the list. The
 * register action is offered to all of them and the API refuses a
 * caller that holds only the read — the same shape the teams section
 * takes.
 */
export default async function OperatorsPage({ params }: Params) {
  const { org } = await params;
  const operators = await listOperators(org);

  return (
    <>
      <PageHeader
        title="Operators"
        description="Operators this organization has registered."
      >
        <Button asChild>
          <Link href={`${orgHref(org, "operators")}/${NEW_ITEM}`}>
            Register operator
          </Link>
        </Button>
      </PageHeader>
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
    </>
  );
}
