import Link from "next/link";

import { cn } from "@/lib/utils";

import { orgHref } from "../../../../_components/navigation";

/** The two views of the Permissions section, as links. */
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
    <nav aria-label="Permissions views">
      <ul className="flex gap-1 border-b">
        {views.map((view) => (
          <li key={view.key}>
            <Link
              href={view.href}
              aria-current={view.key === current ? "page" : undefined}
              className={cn(
                "-mb-px inline-flex h-9 items-center border-b-2 px-3 text-sm font-medium",
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
