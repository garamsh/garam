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
  listOperatorKeys,
  lookupAgentDefinitions,
  lookupOperators,
  lookupOrgMembers,
  slugFromGrn,
} from "@/features/orgs";
import { checkPermissions } from "@/lib/permission-check";

import { ItemCrumb } from "../../../../_components/item-crumb";
import { orgHref } from "../../../../_components/navigation";
import { PageHeader } from "../../../../_components/page-header";
import { DetailPage } from "../../../../_components/templates/detail-page";
import {
  CERTIFICATE_STATUS,
  keyConnection,
  liveKeys,
} from "../_components/operator-status";
import { ControlEndpointForm } from "./_components/control-endpoint-form";
import { DelegationForm } from "./_components/delegation-form";
import { DeleteOperatorForm } from "./_components/delete-operator-form";
import { KeysPanel } from "./_components/keys-panel";
import { NamingDefinitions } from "./_components/naming-definitions";
import { OperatorDetailsForm } from "./_components/operator-details-form";
import { ReissueCredentialForm } from "./_components/reissue-credential-form";

/** Head metadata for an operator's route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Operator — garam" };

type Params = {
  params: Promise<{ org: string; operator: string }>;
  searchParams: Promise<{ tab?: string }>;
};

/** A settings card inside a tab: a title, what it is for, and its form. */
function SettingsCard({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children: React.ReactNode;
}) {
  return (
    <section className="rounded-xl border bg-card">
      <div className="flex flex-col gap-1 border-b px-4 py-3">
        <h2 className="text-base">{title}</h2>
        <p className="text-muted-foreground">{description}</p>
      </div>
      <div className="p-4">{children}</div>
    </section>
  );
}

/** What the page says where garam did not show the operator. */
function Unavailable({ children }: { children: React.ReactNode }) {
  return (
    <>
      <PageHeader
        title="Operator"
        description="An operator of this organization."
      />
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
 * none. Nothing here is a report from the operator: garam never dials
 * one (ADR-0012), so its image and last contact are unbuilt until the
 * Operator reports them (garam-agent-operator#357), and a key's last
 * sign-in is the one sign of it.
 *
 * The page asks the server once which of its actions the caller may
 * take, and leaves out only what it is told is denied (ADR-0103).
 */
export default async function OperatorPage({ params, searchParams }: Params) {
  const [{ org, operator }, { tab }] = await Promise.all([
    params,
    searchParams,
  ]);
  const [
    organization,
    record,
    keys,
    definitions,
    delegation,
    operators,
    members,
    permissions,
  ] = await Promise.all([
    getOrg(org),
    getOperator(org, operator),
    listOperatorKeys(org, operator),
    lookupAgentDefinitions(org),
    getOperatorDelegation(org, operator),
    lookupOperators(org),
    lookupOrgMembers(org),
    checkPermissions(org, {
      register: { action: "operator:register" },
      reissue: { action: "operator:reissue" },
      update: { action: "operator:update" },
      delegate: { action: "operator:delegate" },
      delete: { action: "operator:delete" },
    }),
  ]);

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
        <Banner tone="info" title="You cannot view this operator">
          garam refused to show this operator to you. Access to it is granted by
          this organization&apos;s roles and policies.
        </Banner>
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
  // The first key of an operator never keyed is registering's to issue;
  // any other is re-issuing's (ADR-0102).
  const canIssue =
    keyList !== null && keyList.length === 0
      ? allowed("register")
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
          {
            term: "Image",
            description: (
              <span className="text-muted-foreground">
                Unbuilt — the Operator does not report it yet
              </span>
            ),
          },
          {
            term: "Last contact",
            description: (
              <span className="text-muted-foreground">
                Unbuilt — the Operator does not report it yet
              </span>
            ),
          },
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
      <Banner tone="info" title="Keys are not listed to you">
        garam refused to list this operator&apos;s keys to you.
      </Banner>
    ) : (
      <Banner tone="warn" title={API_UNREACHABLE_TITLE}>
        <ApiUnreachableReason subject="this operator's keys" />
      </Banner>
    );

  const controlTab = (
    <div className="flex flex-col gap-4">
      <p className="max-w-xl text-muted-foreground">
        A hosted operator&apos;s control service holds agent templates,
        execution profiles and execution. The console hands it one operation at
        a time, only at the endpoint registered here.
      </p>
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
                <span className="text-muted-foreground">None registered</span>
              ),
            },
          ]}
        />
      )}
    </div>
  );

  const delegationTab =
    delegation.status === "unreachable" ? (
      <Banner tone="warn" title={API_UNREACHABLE_TITLE}>
        <ApiUnreachableReason subject="this operator's delegation" />
      </Banner>
    ) : delegation.status === "forbidden" ? (
      <Banner tone="info" title="The delegation is not shown to you">
        garam refused to show this operator&apos;s delegation to you.
      </Banner>
    ) : (
      <DelegationForm
        org={org}
        operator={operator}
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
        <SettingsCard
          title="Name and description"
          description="What this organization calls the operator. Its identifier cannot change."
        >
          <OperatorDetailsForm
            org={org}
            operator={operator}
            name={o.name}
            description={o.description}
          />
        </SettingsCard>
      )}
      {allowed("reissue") && (
        <SettingsCard
          title="Legacy credential"
          description="For an operator not yet on keys. Re-issuing voids the current credential and its tokens."
        >
          <ReissueCredentialForm org={org} operator={operator} name={o.name} />
        </SettingsCard>
      )}
      {!allowed("update") && !allowed("reissue") && (
        <p className="text-muted-foreground">
          You can view this operator but not change it.
        </p>
      )}
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
          { value: "overview", label: "Overview", content: overview },
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
                  description:
                    "Stops it authenticating to garam. Work already running in its cluster does not stop.",
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
    </>
  );
}
