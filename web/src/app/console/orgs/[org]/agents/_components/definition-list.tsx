import Link from "next/link";
import { Fragment } from "react";

import { Card, CardTitle } from "@/components/ui/card";
import {
  slugFromGrn,
  type AgentDefinition,
  type Operator,
} from "@/features/orgs";

import { orgHref } from "../../../../_components/navigation";

/**
 * The organization's agent definitions: the agent each one minted, the
 * operator that is to run it, the values that operator constructs it
 * from, and who said so when.
 *
 * `definedBy` is a user GRN and the record carries no name beside it
 * (`api/openapi.yaml` §AgentDefinition), so the identifier is
 * monospaced as an address and the list says what it is rather than
 * leaving an identity subject to read as somebody's name.
 *
 * The operator is shown by the name the organization gave it, with the
 * identifier the definition stores under it and a link to its page
 * (ADR-0074). One not among `operators` — a list refused to the
 * caller — is shown by identifier alone.
 */
export function DefinitionList({
  org,
  definitions,
  operators,
}: {
  org: string;
  definitions: AgentDefinition[];
  operators: Operator[];
}) {
  const names = new Map(
    operators.map((operator) => [operator.grn, operator.name]),
  );
  return (
    <div className="flex flex-col gap-3">
      <ul className="flex flex-col gap-3">
        {definitions.map((definition) => (
          <li key={definition.agentGrn}>
            <Card className="gap-4 p-4">
              <CardTitle className="font-mono text-base">
                {slugFromGrn(definition.agentGrn)}
              </CardTitle>
              <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
                <dt className="text-muted-foreground">Operator</dt>
                <dd>
                  <OperatorName
                    org={org}
                    identifier={slugFromGrn(definition.operatorGrn)}
                    name={names.get(definition.operatorGrn)}
                  />
                </dd>
                <dt className="text-muted-foreground">Defined by</dt>
                <dd className="font-mono">
                  {slugFromGrn(definition.definedBy)}
                </dd>
                <dt className="text-muted-foreground">Defined</dt>
                <dd>
                  <time dateTime={definition.createdAt}>
                    {definition.createdAt.slice(0, 10)}
                  </time>
                </dd>
              </dl>
              <Values values={definition.values} />
            </Card>
          </li>
        ))}
      </ul>
      <p className="max-w-2xl text-sm text-muted-foreground">
        Defined by is the user identifier the record holds; it carries no
        display name.
      </p>
    </div>
  );
}

/** An operator by name and identifier, or by identifier alone. */
function OperatorName({
  org,
  identifier,
  name,
}: {
  org: string;
  identifier: string;
  name: string | undefined;
}) {
  if (name === undefined) {
    return <span className="font-mono">{identifier}</span>;
  }
  return (
    <>
      <Link
        href={`${orgHref(org, "operators")}/${identifier}`}
        className="wrap-anywhere underline-offset-4 hover:underline focus-visible:underline"
      >
        {name}
      </Link>
      <span className="block font-mono text-xs text-muted-foreground">
        {identifier}
      </span>
    </>
  );
}

/** A definition's values, or the fact that it carries none. */
function Values({ values }: { values: Record<string, string> }) {
  const entries = Object.entries(values);
  if (entries.length === 0) {
    return <p className="text-sm text-muted-foreground">No values.</p>;
  }
  return (
    <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 border-t pt-3 font-mono text-xs">
      {entries.map(([key, value]) => (
        <Fragment key={key}>
          <dt className="text-muted-foreground">{key}</dt>
          <dd className="wrap-anywhere whitespace-pre-wrap">{value}</dd>
        </Fragment>
      ))}
    </dl>
  );
}
