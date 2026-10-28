import Link from "next/link";

import { cn } from "@/lib/utils";

import { orgHref } from "../../../../_components/navigation";

/**
 * The two views of the Permissions section. They are addresses, not
 * panels of one page, so they are links marked with the current one,
 * drawn as the line tabs a detail page uses.
 */
export function PermissionsViews({
  org,
  current,
}: {
  org: string;
  current: "policies" | "attachments";
}) {
  const base = orgHref(org, "permissions");
  const views = [
    { key: "policies", label: "Policies", href: base },
    { key: "attachments", label: "Attachments", href: `${base}/attachments` },
  ] as const;
  return (
    <nav aria-label="Permissions views" className="border-b">
      <ul className="-mb-px flex gap-4">
        {views.map((view) => (
          <li key={view.key}>
            <Link
              href={view.href}
              aria-current={view.key === current ? "page" : undefined}
              className={cn(
                "inline-flex h-9 items-center border-b-2 px-1 font-medium outline-none transition-colors focus-visible:rounded-sm focus-visible:ring-3 focus-visible:ring-ring/50",
                view.key === current
                  ? "border-foreground text-foreground"
                  : "border-transparent text-muted-foreground hover:text-foreground",
              )}
            >
              {view.label}
            </Link>
          </li>
        ))}
      </ul>
    </nav>
  );
}
