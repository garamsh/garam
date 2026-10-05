import Link from "next/link";

import { Card, CardTitle } from "@/components/ui/card";
import type { Agent, DirectoryAgent } from "@/features/orgs";
import { cn } from "@/lib/utils";

import { OperatorName } from "./agent-facts";
import { ReportedState } from "./reported-state";

/** One row of the roster: an agent and what the page knows of it. */
export type RosterEntry = {
  directory: DirectoryAgent;
  /** The reported agent, where an operator reported one. */
  agent: Agent | undefined;
  /** The operator a definition names, where one does. */
  operator: { identifier: string; name: string | undefined } | undefined;
};

/**
 * The organization's agents: every defined agent, claimed or not, and
 * every reported agent no definition read names. Each links to its
 * page; one with no definition, as a control service creates, is named
 * by its identifier. A reported one without a name is said to have no
 * definition only where the definitions were read: where they were
 * refused, one may exist unread.
 */
export function AgentRoster({
  org,
  entries,
  definitionsRead,
}: {
  org: string;
  entries: readonly RosterEntry[];
  definitionsRead: boolean;
}) {
  return (
    <ul className="flex flex-col gap-3">
      {entries.map(({ directory, agent, operator }) => (
        <li key={directory.grn}>
          <Card className="gap-3 p-4">
            <div className="flex flex-col gap-0.5">
              <CardTitle className="text-base">
                <Link
                  href={`/console/orgs/${org}/agents/${directory.identifier}`}
                  className={cn(
                    "wrap-anywhere underline-offset-4 hover:underline focus-visible:underline",
                    directory.name === null && "font-mono",
                  )}
                >
                  {directory.name ?? directory.identifier}
                </Link>
              </CardTitle>
              {directory.name === null ? (
                <p className="text-xs text-muted-foreground">
                  {definitionsRead
                    ? "Reported by an operator without a definition here."
                    : "Reported by an operator. Its definition is not checked."}
                </p>
              ) : (
                <p className="font-mono text-xs text-muted-foreground">
                  {directory.identifier}
                </p>
              )}
            </div>
            {operator && (
              <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
                <dt className="text-muted-foreground">Operator</dt>
                <dd>
                  <OperatorName org={org} {...operator} />
                </dd>
              </dl>
            )}
            <ReportedState reported={directory.reported} agent={agent} />
          </Card>
        </li>
      ))}
    </ul>
  );
}
