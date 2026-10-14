import type { Metadata } from "next";

import {
  API_UNREACHABLE_TITLE,
  ApiUnreachableReason,
} from "@/components/api-unreachable";
import { Banner } from "@/components/banner";
import { getOperator, listOperatorKeys } from "@/features/orgs";
import { checkPermissions } from "@/lib/permission-check";

import { ItemCrumb } from "../../../../../_components/item-crumb";
import { orgHref } from "../../../../../_components/navigation";
import { PageHeader } from "../../../../../_components/page-header";
import { liveKeys } from "../../_components/operator-status";
import { ConnectWizard, type ConnectStep } from "./_components/connect-wizard";

/** Head metadata for an operator's setup; Next.js reads it by name. */
export const metadata: Metadata = { title: "Connect an operator — garam" };

type Params = {
  params: Promise<{ org: string; operator: string }>;
  searchParams: Promise<{ rotate?: string }>;
};

/**
 * Connecting one registered identity of an Operator: issue its key,
 * place it, and see it sign in (ADR-0102). The step it opens on is read
 * from garam, so it resumes: no live key opens on issuing, a live key
 * never used on placing, and a key that has signed in on a verified
 * last step. `?rotate` opens on issuing a replacement.
 *
 * It sits below the operator because an identifier may be `new`, and a
 * create route beside the operators would take that operator's page
 * (ADR-0074); `connect` is a page of the operator's, not an identifier.
 */
export default async function ConnectOperatorPage({
  params,
  searchParams,
}: Params) {
  const [{ org, operator }, { rotate }] = await Promise.all([
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

  const header = (
    <PageHeader
      title="Connect an operator"
      description="Issue its key, place it, and see it sign in."
    />
  );
  if (record.status === "unreachable" || keys.status === "unreachable") {
    return (
      <>
        {header}
        <Banner tone="warn" title={API_UNREACHABLE_TITLE}>
          <ApiUnreachableReason subject="this operator's setup" />
        </Banner>
      </>
    );
  }
  if (record.status === "forbidden" || keys.status === "forbidden") {
    return (
      <>
        {header}
        <Banner tone="info" title="You cannot set up this operator">
          garam refused to show this operator or its keys to you.
        </Banner>
      </>
    );
  }

  const o = record.data;
  const live = liveKeys(keys.data);
  const latest = live.at(-1);
  const replacing = rotate !== undefined;
  const initialStep: Exclude<ConnectStep, "register"> =
    replacing || latest === undefined
      ? "issue"
      : latest.lastExchangedAt !== undefined
        ? "verify"
        : "place";
  // The first key of an operator never keyed is registering's to issue;
  // any other is re-issuing's (ADR-0102).
  const issuing = keys.data.length === 0 ? "register" : "reissue";

  if (initialStep === "issue" && permissions.get(issuing) === "denied") {
    return (
      <>
        <ItemCrumb label={o.name} />
        {header}
        <Banner tone="info" title="You cannot issue keys for this operator">
          garam does not let you issue this operator a key. Access to it is
          granted by this organization&apos;s roles and policies.
        </Banner>
      </>
    );
  }

  const operatorHref = `${orgHref(org, "operators")}/${operator}`;
  return (
    <>
      <ItemCrumb label={o.name} />
      <ConnectWizard
        org={org}
        operator={operator}
        name={o.name}
        clientId={o.grn}
        keys={keys.data}
        initialStep={initialStep}
        rotate={replacing}
        operatorHref={operatorHref}
        operatorsHref={orgHref(org, "operators")}
      />
    </>
  );
}
