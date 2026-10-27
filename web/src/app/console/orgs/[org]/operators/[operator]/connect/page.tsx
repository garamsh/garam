import type { Metadata } from "next";

import { Banner } from "@/components/banner";
import {
  getOperator,
  listOperatorKeys,
  type OperatorKey,
} from "@/features/orgs";
import { checkPermissions } from "@/lib/permission-check";

import { ItemCrumb } from "../../../../../_components/item-crumb";
import { orgHref } from "../../../../../_components/navigation";
import { PageHeader } from "../../../../../_components/page-header";
import { liveKeys } from "../../_components/operator-status";
import { ConnectWizard, type ConnectStep } from "./_components/connect-wizard";
import { PairConnectWizard } from "./_components/pair-connect-wizard";

/** Head metadata for an operator's setup; Next.js reads it by name. */
export const metadata: Metadata = { title: "Connect an operator — garam" };

type Params = {
  params: Promise<{ org: string; operator: string }>;
  searchParams: Promise<{ rotate?: string; pair?: string }>;
};

/**
 * Connecting one registered identity of an Operator: issue its key,
 * place it, and see it sign in (ADR-0102). The step it opens on is read
 * from garam, so it resumes: no live key opens on issuing, a live key
 * never used on placing, and a key that has signed in on a verified
 * last step. `?rotate` opens on issuing a replacement.
 *
 * `?pair=<control service>` sets up a cluster manager and its control
 * service as one Operator (ADR-0124). The choice is read from the
 * address alone, never from a read: issuing re-renders this page, and
 * a read that failed on that render would swap the wizard and unmount
 * the one copy of a private key garam answered once.
 *
 * It sits below the operator because an identifier may be `new`, and a
 * create route beside the operators would take that operator's page
 * (ADR-0074); `connect` is a page of the operator's, not an identifier.
 */
export default async function ConnectOperatorPage({
  params,
  searchParams,
}: Params) {
  const [{ org, operator }, { rotate, pair }] = await Promise.all([
    params,
    searchParams,
  ]);
  const [record, keys, permissions] = await Promise.all([
    getOperator(org, operator),
    listOperatorKeys(org, operator),
    checkPermissions(org, {
      register: { action: "operator:register" },
      reissue: { action: "operator:reissue" },
    }),
  ]);
  // garam issues a key to an operator holding no live one under either
  // action, and any other under re-issuing alone
  // (`internal/api/operator_key.go`).
  const denied = (action: "register" | "reissue") =>
    permissions.get(action) === "denied";

  if (pair !== undefined && rotate === undefined) {
    const [control, controlKeys] = await Promise.all([
      getOperator(org, pair),
      listOperatorKeys(org, pair),
    ]);
    const reads = [record, keys, control, controlKeys];
    const failed = reads.find((read) => read.status !== "ok");
    const managerKeys = keys.status === "ok" ? keys.data : [];
    const pairKeys = controlKeys.status === "ok" ? controlKeys.data : [];
    const name = record.status === "ok" ? record.data.name : operator;
    return (
      <>
        <ItemCrumb label={name} />
        <PairConnectWizard
          org={org}
          name={name}
          identities={{
            manager: {
              operator,
              name,
              clientId: record.status === "ok" ? record.data.grn : undefined,
              keys: managerKeys,
            },
            control: {
              operator: pair,
              name: control.status === "ok" ? control.data.name : pair,
              clientId: control.status === "ok" ? control.data.grn : undefined,
              keys: pairKeys,
            },
          }}
          problem={failed === undefined ? null : failed.status}
          initialStep={
            failed === undefined ? pairStep([managerKeys, pairKeys]) : "issue"
          }
          canIssue={!(denied("register") && denied("reissue"))}
          operatorsHref={orgHref(org, "operators")}
        />
      </>
    );
  }

  const header = <PageHeader title="Connect an operator" />;

  // A read that fails is shown inside the wizard rather than in its
  // place: the issuing action's re-render runs this page again, and
  // swapping the wizard out would unmount the one copy of a private key
  // garam answered once.
  const problem =
    record.status !== "ok"
      ? record.status
      : keys.status !== "ok"
        ? keys.status
        : null;
  const keyList = keys.status === "ok" ? keys.data : [];
  const name = record.status === "ok" ? record.data.name : operator;
  const live = liveKeys(keyList);
  const latest = live.at(-1);
  const replacing = rotate !== undefined;
  const initialStep: Exclude<ConnectStep, "register"> =
    problem !== null || replacing || latest === undefined
      ? "issue"
      : latest.lastExchangedAt !== undefined
        ? "verify"
        : "place";
  const canIssue =
    live.length === 0
      ? !(denied("register") && denied("reissue"))
      : !denied("reissue");

  if (problem === null && initialStep === "issue" && !canIssue) {
    return (
      <>
        <ItemCrumb label={name} />
        {header}
        <Banner
          tone="info"
          title="No policy lets you issue this identity a key"
        />
      </>
    );
  }

  const operatorHref = `${orgHref(org, "operators")}/${operator}`;
  return (
    <>
      <ItemCrumb label={name} />
      <ConnectWizard
        org={org}
        operator={operator}
        name={name}
        clientId={record.status === "ok" ? record.data.grn : undefined}
        keys={keyList}
        problem={problem}
        initialStep={initialStep}
        canReplace={!denied("reissue")}
        isControlService={
          record.status === "ok" && record.data.manager !== undefined
        }
        operatorHref={operatorHref}
        operatorsHref={orgHref(org, "operators")}
      />
    </>
  );
}

/**
 * Where a pair's setup resumes: issuing while either identity holds no
 * live key, verifying once both have signed in, and placing between.
 */
function pairStep(
  keys: [OperatorKey[], OperatorKey[]],
): Exclude<ConnectStep, "register"> {
  const latest = keys.map((held) => liveKeys(held).at(-1));
  if (latest.some((key) => key === undefined)) return "issue";
  return latest.every((key) => key?.lastExchangedAt !== undefined)
    ? "verify"
    : "place";
}
