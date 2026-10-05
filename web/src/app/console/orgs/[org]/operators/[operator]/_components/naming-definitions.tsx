import Link from "next/link";

import { Button } from "@/components/ui/button";
import { slugFromGrn, type AgentDefinition } from "@/features/orgs";

import { CutoverBadge } from "../../../../../_components/cutover-marker";

/**
 * The agent definitions naming this operator, which is what refuses its
 * deletion (ADR-0045), and the route to the page where they are read in
 * full. A definition says which operator is to run its agent, so the
 * registration cannot go while one still says it.
 *
 * A refused list is said as such rather than as none: "no definition
 * names it" would be a claim this page could not check.
 */
export function NamingDefinitions({
  definitions,
  agentsHref,
}: {
  definitions:
    { status: "ok"; data: AgentDefinition[] } | { status: "forbidden" };
  agentsHref: string;
}) {
  return (
    <section className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="font-heading text-lg font-semibold">
          Agent definitions
        </h2>
        <Button asChild variant="outline" size="sm">
          <Link href={`${agentsHref}#definitions`}>View in Agents</Link>
        </Button>
      </div>
      {definitions.status === "forbidden" ? (
        <p className="max-w-2xl text-sm text-muted-foreground">
          Not checked: agent definitions are not listed to you.
        </p>
      ) : definitions.data.length === 0 ? (
        <p className="max-w-2xl text-sm text-muted-foreground">
          No agent definition names this operator.
        </p>
      ) : (
        <>
          <p className="max-w-2xl text-sm text-muted-foreground">
            Deleting is refused while{" "}
            {definitions.data.length === 1
              ? "this definition names"
              : `these ${definitions.data.length} definitions name`}{" "}
            this operator.
          </p>
          <ul className="flex flex-col gap-1 text-sm">
            {definitions.data.map((definition) => (
              <li key={definition.agentGrn} className="flex flex-wrap gap-x-3">
                <Link
                  href={`${agentsHref}/${slugFromGrn(definition.agentGrn)}`}
                  className="wrap-anywhere underline-offset-4 hover:underline focus-visible:underline"
                >
                  {definition.name}
                </Link>
                <span className="font-mono text-muted-foreground">
                  {slugFromGrn(definition.agentGrn)}
                </span>
                <span className="text-muted-foreground">
                  defined{" "}
                  <time dateTime={definition.createdAt}>
                    {definition.createdAt.slice(0, 10)}
                  </time>
                </span>
                <CutoverBadge cutover={definition.cutover} />
              </li>
            ))}
          </ul>
        </>
      )}
    </section>
  );
}
