import type { Metadata } from "next";

import { Separator } from "@/components/ui/separator";
import {
  getOperator,
  getOperatorDelegation,
  getOrg,
  lookupAgentDefinitions,
  lookupOperators,
  slugFromGrn,
  type Operator,
} from "@/features/orgs";

import { ApiUnreachable } from "../../../../_components/api-unreachable";
import { ItemCrumb } from "../../../../_components/item-crumb";
import { orgHref } from "../../../../_components/navigation";
import { PageHeader } from "../../../../_components/page-header";
import { EnrollmentBadge } from "../_components/enrollment-badge";
import { ControlEndpointForm } from "./_components/control-endpoint-form";
import { DelegationForm } from "./_components/delegation-form";
import { DeleteOperatorForm } from "./_components/delete-operator-form";
import { NamingDefinitions } from "./_components/naming-definitions";
import { OperatorDetailsForm } from "./_components/operator-details-form";

/** Head metadata for an operator's route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Operator — garam" };

type Params = { params: Promise<{ org: string; operator: string }> };

/** What each enrollment state is, as a label beside its badge. */
const ENROLLMENT_MEANING: Record<Operator["enrollmentState"], string> = {
  enrolled: "Certificate issued",
  pending: "Enrollment token available",
  expired: "No certificate or enrollment token",
};

/**
 * One operator: its identity, where it stands in obtaining a
 * certificate, the definitions that name it, and its settings
 * (ADR-0074).
 *
 * The organization is read for itself, so one the caller cannot read
 * answers 404 (ADR-0011). Inside one it can read, a refused operator
 * says so, and a refused list of definitions is said as unchecked
 * rather than as none.
 *
 * Nothing here is a report from the operator: garam never dials one
 * (ADR-0012) and shows no runtime report from it, so runtime is
 * unknown rather than inferred from enrollment. The GRN is not printed
 * (ADR-0011); the identifier is what tells two operators of one name
 * apart.
 *
 * The write controls are rendered for every reader and the API refuses
 * a caller that holds only the read, as in the other sections.
 */
export default async function OperatorPage({ params }: Params) {
  const { org, operator } = await params;
  const [organization, record, definitions, delegation, operators] =
    await Promise.all([
      getOrg(org),
      getOperator(org, operator),
      lookupAgentDefinitions(org),
      getOperatorDelegation(org, operator),
      lookupOperators(org),
    ]);

  if (
    organization.status === "unreachable" ||
    record.status === "unreachable" ||
    definitions.status === "unreachable"
  ) {
    return (
      <>
        <PageHeader
          title="Operator"
          description="An operator of this organization."
        />
        <ApiUnreachable subject="this operator" />
      </>
    );
  }
  if (record.status === "forbidden") {
    return (
      <>
        <PageHeader
          title="Operator"
          description="An operator of this organization."
        />
        <section role="alert" className="flex flex-col gap-1">
          <h2 className="font-heading text-lg font-semibold">
            You cannot view this operator
          </h2>
          <p className="text-sm text-muted-foreground">
            garam refused to show this operator to you. Access to it is granted
            by this organization&apos;s roles and policies.
          </p>
        </section>
      </>
    );
  }

  const o = record.data;
  const naming =
    definitions.status === "forbidden"
      ? definitions
      : {
          status: "ok" as const,
          data: definitions.data.filter(
            (definition) => definition.operatorGrn === o.grn,
          ),
        };

  return (
    <>
      <ItemCrumb label={o.name} />
      <PageHeader
        title={o.name}
        description={o.description || "An operator of this organization."}
      />
      <section className="flex flex-col gap-3">
        <h2 className="font-heading text-lg font-semibold">Identity</h2>
        <dl className="grid grid-cols-1 gap-x-4 gap-y-1.5 text-sm sm:grid-cols-[auto_1fr]">
          <dt className="text-muted-foreground">Identifier</dt>
          <dd className="font-mono break-all">{operator}</dd>
          <dt className="text-muted-foreground">Registered by</dt>
          <dd className="font-mono break-all">{slugFromGrn(o.registeredBy)}</dd>
          <dt className="text-muted-foreground">Registered</dt>
          <dd>
            <time dateTime={o.createdAt}>{o.createdAt.slice(0, 10)}</time>
          </dd>
        </dl>
      </section>
      <section className="flex flex-col gap-3">
        <h2 className="font-heading text-lg font-semibold">Enrollment</h2>
        <dl className="grid grid-cols-1 items-center gap-x-4 gap-y-1.5 text-sm sm:grid-cols-[auto_1fr]">
          <dt className="text-muted-foreground">State</dt>
          <dd className="flex flex-wrap items-center gap-2">
            <EnrollmentBadge state={o.enrollmentState} />
            <span>{ENROLLMENT_MEANING[o.enrollmentState]}</span>
          </dd>
          {o.certificateIssuedAt && (
            <>
              <dt className="text-muted-foreground">Last certificate</dt>
              <dd>
                <time dateTime={o.certificateIssuedAt}>
                  {o.certificateIssuedAt.slice(0, 10)}
                </time>
              </dd>
            </>
          )}
          <dt className="text-muted-foreground">Runtime</dt>
          <dd>Unknown — no report</dd>
        </dl>
      </section>
      <NamingDefinitions
        definitions={naming}
        agentsHref={orgHref(org, "agents")}
      />
      <Separator />
      <section className="flex flex-col gap-4">
        <h2 className="font-heading text-lg font-semibold">Control service</h2>
        <p className="max-w-2xl text-sm text-muted-foreground">
          A hosted operator&apos;s control service holds agent templates,
          execution profiles and execution. garam hands it one operation at a
          time, under the delegation below and only at the registered endpoint.
        </p>
        <div className="flex max-w-2xl flex-col gap-2">
          <h3 className="text-sm font-medium">Endpoint</h3>
          <ControlEndpointForm
            org={org}
            operator={operator}
            endpoint={o.controlEndpoint}
          />
        </div>
        <div className="flex max-w-2xl flex-col gap-2">
          <h3 className="text-sm font-medium">Delegation</h3>
          {delegation.status === "unreachable" ||
          delegation.status === "forbidden" ? (
            <p role="alert" className="text-sm text-muted-foreground">
              {delegation.status === "forbidden"
                ? "garam refused to show this operator's delegation to you."
                : "garam did not answer for this operator's delegation."}
            </p>
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
          )}
        </div>
      </section>
      <Separator />
      <section className="flex flex-col gap-4">
        <h2 className="font-heading text-lg font-semibold">Settings</h2>
        <OperatorDetailsForm
          org={org}
          operator={operator}
          name={o.name}
          description={o.description}
        />
        <div className="flex max-w-2xl flex-col gap-2">
          <h3 className="text-sm font-medium">Delete registration</h3>
          <DeleteOperatorForm
            org={org}
            operator={operator}
            name={o.name}
            enrolled={o.enrollmentState === "enrolled"}
          />
        </div>
      </section>
    </>
  );
}
