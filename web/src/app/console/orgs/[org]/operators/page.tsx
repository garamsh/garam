import type { Metadata } from "next";

import { Banner } from "@/components/banner";
import {
  listHostedOperators,
  lookupOperators,
  slugFromGrn,
} from "@/features/orgs";
import { checkPermissions } from "@/lib/permission-check";

import {
  API_UNREACHABLE_TITLE,
  ApiUnreachableReason,
} from "@/components/api-unreachable";
import { orgHref } from "../../../_components/navigation";
import { ListPage } from "../../../_components/templates/list-page";
import { ConnectOperatorDialog } from "./_components/connect-operator-dialog";
import { OperatorTable } from "./_components/operator-table";
import { reportsSource } from "./_components/reports-source";

/** Head metadata for the operators route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Operators — garam" };

type Params = {
  params: Promise<{ org: string }>;
  searchParams: Promise<{ connect?: string }>;
};

/**
 * The operators the organization has registered, each opening a page
 * of its own, and the way to connect one more (ADR-0074, ADR-0102).
 *
 * Every role reads the list; connecting starts by registering, which is
 * `operator:register`. The page asks the server once and leaves the
 * control out only where the answer is a denial, so a check that could
 * not be asked still offers it and the API refuses whoever holds only
 * the read. `?connect` opens the dialog, which is where an operator's
 * own setup sends a person to connect its second identity.
 *
 * What each operator reports running and when it last made contact is
 * the hosted operator's control service's to answer (#1047), read in
 * the browser under `operator:read`, and left out only where the
 * server denies the caller that read.
 */
export default async function OperatorsPage({ params, searchParams }: Params) {
  const [{ org }, { connect }] = await Promise.all([params, searchParams]);
  const [operators, hosted, permissions] = await Promise.all([
    lookupOperators(org),
    listHostedOperators(org),
    checkPermissions(org, {
      register: { action: "operator:register" },
      readReports: { action: "operator:read" },
    }),
  ]);
  const operatorsHref = orgHref(org, "operators");

  return (
    <ListPage
      title="Operators"
      description="The Operators that run this organization's agents in its clusters. Each connects two identities: its manager and its control service."
      count={operators.status === "ok" ? operators.data.length : undefined}
      primaryAction={
        permissions.get("register") !== "denied" && (
          <ConnectOperatorDialog
            org={org}
            operatorsHref={operatorsHref}
            defaultOpen={connect !== undefined}
          />
        )
      }
    >
      {operators.status === "unreachable" ? (
        <Banner tone="warn" title={API_UNREACHABLE_TITLE}>
          <ApiUnreachableReason subject="this organization's operators" />
        </Banner>
      ) : operators.status === "forbidden" ? (
        <Banner tone="info" title="Operators are not listed to you">
          garam refused to list this organization&apos;s operators to you.
          Access to them is granted by the organization&apos;s roles and
          policies.
        </Banner>
      ) : (
        <OperatorTable
          operatorsHref={operatorsHref}
          reports={
            permissions.get("readReports") === "denied"
              ? null
              : reportsSource(org, hosted)
          }
          operators={operators.data.map((operator) => ({
            grn: operator.grn,
            identifier: slugFromGrn(operator.grn),
            name: operator.name,
            enrollmentState: operator.enrollmentState,
            controlEndpoint: operator.controlEndpoint,
            createdAt: operator.createdAt,
          }))}
        />
      )}
    </ListPage>
  );
}
