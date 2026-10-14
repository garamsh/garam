import Link from "next/link";

import { Time } from "@/components/time";
import {
  slugFromGrn,
  type AgentDefinition,
  type Catalog,
} from "@/features/orgs";

import { CutoverBadge } from "../../../../../_components/cutover-marker";

/**
 * The agent definitions naming this operator, which is what refuses its
 * deletion (ADR-0045). A definition says which operator is to run its
 * agent, so the registration cannot go while one still says it.
 *
 * A refused list is said as such rather than as none: "no definition
 * names it" would be a claim this page could not check. Definitions are
 * named and linked, never shown by identifier.
 */
export function NamingDefinitions({
  definitions,
  agentsHref,
}: {
  definitions: Catalog<AgentDefinition>;
  agentsHref: string;
}) {
  return (
    <section
      aria-labelledby="naming-definitions"
      className="flex flex-col gap-3"
    >
      <h2 id="naming-definitions" className="text-base">
        Agents it runs
      </h2>
      {definitions.status === "forbidden" ? (
        <p className="text-muted-foreground">
          Not checked: agent definitions are not listed to you.
        </p>
      ) : definitions.data.length === 0 ? (
        <p className="text-muted-foreground">
          No agent definition names this operator.
        </p>
      ) : (
        <ul className="flex flex-col divide-y rounded-xl border">
          {definitions.data.map((definition) => (
            <li
              key={definition.agentGrn}
              className="flex flex-wrap items-center gap-x-3 gap-y-1 px-4 py-2.5"
            >
              <Link
                href={`${agentsHref}/${slugFromGrn(definition.agentGrn)}`}
                className="font-medium wrap-anywhere underline-offset-4 hover:underline"
              >
                {definition.name}
              </Link>
              <CutoverBadge cutover={definition.cutover} />
              <span className="ml-auto text-muted-foreground">
                Defined <Time value={definition.createdAt} />
              </span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
