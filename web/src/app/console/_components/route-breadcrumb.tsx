"use client";

import { usePathname } from "next/navigation";
import { Fragment } from "react";

import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";
import { cn } from "@/lib/utils";

import { IntentLink } from "./intent-link";
import { useItemCrumb } from "./item-crumb";
import { useMissing } from "./missing-page";
import {
  consoleCrumbs,
  isUnknownOrg,
  notFoundCrumbs,
  parseConsolePath,
  type ConsoleOrg,
} from "./navigation";

/**
 * The trail to the current route. Narrow viewports keep the
 * organization's name and the crumb naming the page on screen, so the
 * organization a page acts on is never out of sight. `orgs` is null
 * when the API did not answer, and the trail then names organizations
 * by slug.
 */
export function RouteBreadcrumb({
  orgs,
}: {
  orgs: readonly ConsoleOrg[] | null;
}) {
  const pathname = usePathname();
  const location = parseConsolePath(pathname);
  const itemName = useItemCrumb(pathname);
  const missing = useMissing(pathname) || isUnknownOrg(location, orgs);
  const crumbs = missing
    ? notFoundCrumbs(location, orgs)
    : consoleCrumbs(location, orgs ?? [], itemName);
  // The organization's crumb follows "Organizations" inside one.
  const orgIndex =
    location.kind === "org" && !isUnknownOrg(location, orgs) ? 1 : -1;
  const kept = (index: number) =>
    index === crumbs.length - 1 || index === orgIndex;

  return (
    <Breadcrumb className="min-w-0">
      <BreadcrumbList className="flex-nowrap">
        {crumbs.map((crumb, index) => (
          <Fragment key={`${index}:${crumb.label}`}>
            {index > 0 && (
              <BreadcrumbSeparator
                className={cn(
                  !(kept(index) && orgIndex >= 0 && index > orgIndex) &&
                    "hidden md:block",
                )}
              />
            )}
            <BreadcrumbItem
              className={cn("min-w-0", !kept(index) && "hidden md:inline-flex")}
            >
              {crumb.href ? (
                <BreadcrumbLink asChild>
                  <IntentLink
                    href={crumb.href}
                    className={cn(index === orgIndex && "max-w-40 truncate")}
                  >
                    {crumb.label}
                  </IntentLink>
                </BreadcrumbLink>
              ) : (
                <BreadcrumbPage className="truncate">
                  {crumb.label}
                </BreadcrumbPage>
              )}
            </BreadcrumbItem>
          </Fragment>
        ))}
      </BreadcrumbList>
    </Breadcrumb>
  );
}
