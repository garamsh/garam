import type { Metadata } from "next";
import Link from "next/link";
import { ArrowRight } from "lucide-react";

import {
  API_UNREACHABLE_TITLE,
  ApiUnreachableReason,
} from "@/components/api-unreachable";
import { Banner } from "@/components/banner";
import { DescriptionList } from "@/components/description-list";
import { StatusDot } from "@/components/status-dot";
import { Time } from "@/components/time";
import { Button } from "@/components/ui/button";
import {
  getOperator,
  getOperatorDelegation,
  getOrg,
  listHostedOperators,
  listOperatorKeys,
  lookupAgentDefinitions,
  lookupOperators,
  lookupOrgMembers,
  slugFromGrn,
} from "@/features/orgs";

import { ItemCrumb } from "../../../../_components/item-crumb";
import { orgHref } from "../../../../_components/navigation";
import { PageHeader } from "../../../../_components/page-header";
import { RefusedRead } from "../../../../_components/refused-read";
import { DetailPage } from "../../../../_components/templates/detail-page";
import {
  YourAccess,
  accessDecisions,
  readAccess,
} from "../../../../_components/your-access";
import {
  OperatorReportsProvider,
  ProvidedImages,
  ProvidedLastContact,
  ProvidedReportsNotice,
} from "../_components/operator-reports";
import {
  CERTIFICATE_STATUS,
  keyConnection,
  liveKeys,
} from "../_components/operator-status";
import { reportsSource } from "../_components/reports-source";
import { ControlEndpointForm } from "./_components/control-endpoint-form";
import { DelegationForm } from "./_components/delegation-form";
import { DeleteOperatorForm } from "./_components/delete-operator-form";
import { KeysPanel } from "./_components/keys-panel";
import { MintEnrollmentForm } from "./_components/mint-enrollment-form";
import { NamingDefinitions } from "./_components/naming-definitions";
import { OperatorDetailsForm } from "./_components/operator-details-form";
import { ReissueCredentialForm } from "./_components/reissue-credential-form";

/** Head metadata for an operator's route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Operator — garam" };

type Params = {
  params: Promise<{ org: string; operator: string }>;
  searchParams: Promise<{ tab?: string }>;
};

/** A settings card inside a tab: a title and its form. */
function SettingsCard({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <section className="rounded-xl border bg-card">
      <h2 className="border-b px-4 py-3 text-base">{title}</h2>
      <div className="p-4">{children}</div>
    </section>
  );
}

/** What the page says where garam did not show the operator. */
function Unavailable({ children }: { children: React.ReactNode }) {
  return (
    <>
      <PageHeader title="Operator" />
      {children}
    </>
  );
}

/**
 * One operator (ADR-0074): what it is, its keys (ADR-0102), its control
 * service and delegation, its settings, and deleting it.
 *
 * The organization is read for itself, so one the caller cannot read
 * answers 404 (ADR-0011). Inside one it can read, a refused operator
 * says so, and a refused list says it was not checked rather than
 * none. garam never dials an operator (ADR-0012), so a key's last
 * sign-in is garam's one sign of it; the images it runs and its last
 * contact are the hosted operator's control service's to report
 * (#1047), read in the browser under `operator:read`.
 *
 * The page asks the server once which of its actions the caller may
 * take, and leaves out only what it is told is denied (ADR-0103).
 */
export default async function OperatorPage({ params, searchParams }: Params) {
  const [{ org, operator }, { tab }] = await Promise.all([
    params,
    searchParams,
  ]);
  // Started before the page's own reads, so it runs beside them.
  const access = readAccess(org, [
    {
      key: "readReports",
      label: "Read its reports",
      check: { action: "operator:read" },
    },
    {
      key: "update",
      label: "Rename it",
      check: { action: "operator:update" },
    },
    {
      key: "register",
      label: "Issue its first key",
      check: { action: "operator:register" },
    },
    {
      key: "reissue",
      label: "Re-issue its keys",
      check: { action: "operator:reissue" },
    },
    {
      key: "delegate",
      label: "Delegate to it",
      check: { action: "operator:delegate" },
    },
    { key: "delete", label: "Delete it", check: { action: "operator:delete" } },
  ]);
  const [
    organization,
    record,
    keys,
    definitions,
    delegation,
    operators,
    members,
    hosted,
    readings,
  ] = await Promise.all([
    getOrg(org),
    getOperator(org, operator),
    listOperatorKeys(org, operator),
    lookupAgentDefinitions(org),
    getOperatorDelegation(org, operator),
    lookupOperators(org),
    lookupOrgMembers(org),
    listHostedOperators(org),
    access,
  ]);
  const permissions = accessDecisions(readings);

  if (
    organization.status === "unreachable" ||
    record.status === "unreachable" ||
    definitions.status === "unreachable"
  ) {
    return (
      <Unavailable>
        <Banner tone="warn" title={API_UNREACHABLE_TITLE}>
          <ApiUnreachableReason subject="this operator" />
        </Banner>
      </Unavailable>
    );
  }
  if (record.status === "forbidden") {
    return (
      <Unavailable>
        <RefusedRead subject="this operator" />
      </Unavailable>
    );
  }

  const o = record.data;
  const allowed = (name: Parameters<typeof permissions.get>[0]) =>
    permissions.get(name) !== "denied";
  const agentsHref = orgHref(org, "agents");
  const setupHref = `${orgHref(org, "operators")}/${operator}/connect`;
  const naming =
    definitions.status === "forbidden"
      ? definitions
      : {
          status: "ok" as const,
          data: definitions.data.filter(
            (definition) => definition.operatorGrn === o.grn,
          ),
        };
  const registeredBy =
    members.status === "ok"
      ? members.data.find((member) => member.userGrn === o.registeredBy)
          ?.nickname
      : undefined;
  const keyList = keys.status === "ok" ? keys.data : null;
  const connection = keyList ? keyConnection(keyList) : null;
  // garam issues a key to an operator holding no live one under either
  // action, and any other under re-issuing alone
  // (`internal/api/operator_key.go`).
  const canIssue =
    keyList !== null && liveKeys(keyList).length === 0
      ? allowed("register") || allowed("reissue")
      : allowed("reissue");

  const overview = (
    <div className="flex flex-col gap-8">
      <DescriptionList
        items={[
          {
            term: "Description",
            description: o.description || (
              <span className="text-muted-foreground">None</span>
            ),
          },
          {
            term: "Signs in with",
            description: connection ? (
              <StatusDot {...connection} />
            ) : (
              <span className="text-muted-foreground">Not checked</span>
            ),
          },
          {
            term: "Certificate",
            description: (
              <span className="flex flex-wrap items-center gap-x-1.5">
                <StatusDot {...CERTIFICATE_STATUS[o.enrollmentState]} />
                {o.certificateIssuedAt && (
                  <span className="text-muted-foreground">
                    · last issued <Time value={o.certificateIssuedAt} />
                  </span>
                )}
              </span>
            ),
          },
          ...(allowed("readReports")
            ? [
                {
                  term: "Images",
                  description: <ProvidedImages grn={o.grn} />,
                },
                {
                  term: "Last contact",
                  description: <ProvidedLastContact grn={o.grn} />,
                },
              ]
            : []),
          {
            term: "Registered",
            description: (
              <span>
                <Time value={o.createdAt} />
                {registeredBy && (
                  <span className="text-muted-foreground">
                    {" "}
                    by {registeredBy}
                  </span>
                )}
              </span>
            ),
          },
        ]}
      />
      <NamingDefinitions definitions={naming} agentsHref={agentsHref} />
    </div>
  );
  const overviewTab = allowed("readReports") ? (
    <OperatorReportsProvider source={reportsSource(org, hosted)}>
      <div className="flex flex-col gap-4">
        <ProvidedReportsNotice operatorsHref={orgHref(org, "operators")} />
        {overview}
      </div>
    </OperatorReportsProvider>
  ) : (
    overview
  );

  const keysTab =
    keys.status === "ok" ? (
      <KeysPanel
        org={org}
        operator={operator}
        keys={keys.data}
        setupHref={setupHref}
        canIssue={canIssue}
        canRevoke={allowed("reissue")}
      />
    ) : keys.status === "forbidden" ? (
      <RefusedRead subject="this operator's keys" />
    ) : (
      <Banner tone="warn" title={API_UNREACHABLE_TITLE}>
        <ApiUnreachableReason subject="this operator's keys" />
      </Banner>
    );

  const controlTab = (
    <div className="flex flex-col gap-4">
      {allowed("delegate") ? (
        <ControlEndpointForm
          org={org}
          operator={operator}
          endpoint={o.controlEndpoint}
        />
      ) : (
        <DescriptionList
          items={[
            {
              term: "Endpoint",
              description: o.controlEndpoint ?? (
                <span className="text-muted-foreground">None</span>
              ),
            },
          ]}
        />
      )}
    </div>
  );

  const delegationTab =
    delegation.status === "unreachable" ? (
      <Banner tone="warn" title="Delegation not read" />
    ) : delegation.status === "forbidden" ? (
      <RefusedRead subject="this operator's delegation" />
    ) : (
      <DelegationForm
        org={org}
        operator={operator}
        canChange={allowed("delegate")}
        delegation={delegation.status === "ok" ? delegation.data : null}
        controllers={
          operators.status === "ok"
            ? operators.data
                .filter((c) => c.grn !== o.grn)
                .map((c) => ({
                  grn: c.grn,
                  name: c.name,
                  identifier: slugFromGrn(c.grn),
                }))
            : []
        }
      />
    );

  const settingsTab = (
    <div className="flex flex-col gap-6">
      {allowed("update") && (
        <SettingsCard title="Name">
          <OperatorDetailsForm
            org={org}
            operator={operator}
            name={o.name}
            description={o.description}
          />
        </SettingsCard>
      )}
      {allowed("reissue") && (
        <SettingsCard title="Legacy credential">
          <ReissueCredentialForm org={org} operator={operator} name={o.name} />
        </SettingsCard>
      )}
      <SettingsCard title="Enrollment token">
        <MintEnrollmentForm
          org={org}
          operator={operator}
          name={o.name}
          blocked={
            o.enrollmentState === "enrolled"
              ? "It has already enrolled."
              : allowed("register")
                ? null
                : "No policy lets you issue enrollment tokens."
          }
        />
      </SettingsCard>
    </div>
  );

  return (
    <>
      <ItemCrumb label={o.name} />
      <DetailPage
        name={o.name}
        idLabel="Identifier"
        id={operator}
        status={connection ? <StatusDot {...connection} /> : undefined}
        actions={
          keyList !== null && liveKeys(keyList).length === 0 && canIssue ? (
            <Button asChild>
              <Link href={setupHref}>
                Finish setup
                <ArrowRight />
              </Link>
            </Button>
          ) : undefined
        }
        defaultTab={tab}
        tabs={[
          { value: "overview", label: "Overview", content: overviewTab },
          { value: "keys", label: "Keys", content: keysTab },
          { value: "control", label: "Control service", content: controlTab },
          { value: "delegation", label: "Delegation", content: delegationTab },
          { value: "settings", label: "Settings", content: settingsTab },
        ]}
        dangerZone={
          allowed("delete")
            ? [
                {
                  title: "Delete this operator",
                  control: (
                    <DeleteOperatorForm
                      org={org}
                      operator={operator}
                      name={o.name}
                      blocking={
                        naming.status === "ok"
                          ? naming.data.map((definition) => ({
                              name: definition.name,
                              href: `${agentsHref}/${slugFromGrn(definition.agentGrn)}`,
                            }))
                          : null
                      }
                    />
                  ),
                },
              ]
            : undefined
        }
      />
      <YourAccess readings={readings} />
    </>
  );
}
