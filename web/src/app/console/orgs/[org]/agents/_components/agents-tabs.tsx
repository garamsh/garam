import Link from "next/link";

import { cn } from "@/lib/utils";

/** The two views inside Agents. */
export type AgentsView = "agents" | "templates";

const VIEWS: { view: AgentsView; label: string; path: string }[] = [
  { view: "agents", label: "Agents", path: "" },
  { view: "templates", label: "Templates", path: "/templates" },
];

/**
 * Switches between the organization's agents and its templates, which
 * live inside Agents rather than as a section of their own (ADR-0075).
 */
export function AgentsTabs({
  org,
  current,
}: {
  org: string;
  current: AgentsView;
}) {
  return (
    <nav aria-label="Agents views" className="flex gap-1 border-b">
      {VIEWS.map(({ view, label, path }) => (
        <Link
          key={view}
          href={`/console/orgs/${org}/agents${path}`}
          aria-current={view === current ? "page" : undefined}
          className={cn(
            "-mb-px border-b-2 px-3 py-2 text-sm font-medium",
            view === current
              ? "border-primary text-foreground"
              : "border-transparent text-muted-foreground hover:text-foreground",
          )}
        >
          {label}
        </Link>
      ))}
    </nav>
  );
}
