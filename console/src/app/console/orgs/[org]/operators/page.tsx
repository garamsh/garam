import type { Metadata } from "next";

import { Banner } from "@/components/banner";
import { listHostedOperators, lookupOperators } from "@/features/orgs";
import { checkPermissions } from "@/lib/permission-check";

import {
  API_UNREACHABLE_TITLE,
  ApiUnreachableReason,
} from "@/components/api-unreachable";
import { orgHref } from "../../../_components/navigation";
import { RefusedRead } from "../../../_components/refused-read";
import { ListPage } from "../../../_components/templates/list-page";
import { ConnectOperatorDialog } from "./_components/connect-operator-dialog";
import { pairOperators } from "./_components/operator-pairs";
import { OperatorTable } from "./_components/operator-table";
import { reportsSource } from "./_components/reports-source";

/** Head metadata for the operators route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Operators — garam" };

type Params = {
  params: Promise<{ org: string }>;
  searchParams: Promise<{ connect?: string }>;
};

/**
 * The organization's Operators, one row each: a manager and the control
 * service whose record names it (ADR-0124), each identity opening a
 * page of its own, and the way to connect one more (ADR-0074,
 * ADR-0102).
 *
 * Every role reads the list; connecting starts by registering, which is
 * `operator:register`. The page asks the server once and leaves the
 * control out only where the answer is a denial, so a check that could
 * not be asked still offers it and the API refuses whoever holds only
 * the read. `?connect` opens the dialog, which is where an operator's
 * own setup sends a person to connect its second identity; it offers
 * the managers that have no control service yet, and `?connect=<id>`
 * chooses that one.
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
  const rows = operators.status === "ok" ? pairOperators(operators.data) : [];
  const openManagers = rows.flatMap((row) =>
    row.manager && !row.controlService ? [row.manager] : [],
  );

  return (
    <ListPage
      title="Operators"
      description="Each connects two identities: its cluster manager and its control service."
      count={operators.status === "ok" ? rows.length : undefined}
      primaryAction={
        permissions.get("register") !== "denied" && (
          <ConnectOperatorDialog
            org={org}
            managers={openManagers}
            defaultOpen={connect !== undefined}
            defaultManager={
              openManagers.find((manager) => manager.identifier === connect)
                ?.grn
            }
          />
        )
      }
    >
      {operators.status === "unreachable" ? (
        <Banner tone="warn" title={API_UNREACHABLE_TITLE}>
          <ApiUnreachableReason subject="this organization's operators" />
        </Banner>
      ) : operators.status === "forbidden" ? (
        <RefusedRead subject="this organization's operators" />
      ) : (
        <OperatorTable
          operatorsHref={operatorsHref}
          reports={
            permissions.get("readReports") === "denied"
              ? null
              : reportsSource(org, hosted)
          }
          operators={rows}
        />
      )}
    </ListPage>
  );
}
