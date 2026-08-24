import { Fragment } from "react";

import { Card, CardTitle } from "@/components/ui/card";
import { slugFromGrn, type AgentDefinition } from "@/features/orgs";

/**
 * The organization's agent definitions: the agent each one minted, the
 * operator that is to run it, the values that operator constructs it
 * from, and who said so when.
 */
export function DefinitionList({
  definitions,
}: {
  definitions: AgentDefinition[];
}) {
  return (
    <ul className="flex flex-col gap-3">
      {definitions.map((definition) => (
        <li key={definition.agentGrn}>
          <Card className="gap-4 p-4">
            <CardTitle className="font-mono text-base">
              {slugFromGrn(definition.agentGrn)}
            </CardTitle>
            <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
              <dt className="text-muted-foreground">Operator</dt>
              <dd className="font-mono">
                {slugFromGrn(definition.operatorGrn)}
              </dd>
              <dt className="text-muted-foreground">Defined by</dt>
              <dd>{slugFromGrn(definition.definedBy)}</dd>
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
