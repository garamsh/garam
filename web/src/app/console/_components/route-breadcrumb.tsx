"use client";

import Link from "next/link";
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

import { useItemCrumb } from "./item-crumb";
import { consoleCrumbs, parseConsolePath, type ConsoleOrg } from "./navigation";

/**
 * The trail to the current route. Narrow viewports keep only the last
 * crumb, which is the one naming the page on screen.
 */
export function RouteBreadcrumb({ orgs }: { orgs: ConsoleOrg[] }) {
  const pathname = usePathname();
  const crumbs = consoleCrumbs(
    parseConsolePath(pathname),
    orgs,
    useItemCrumb(pathname),
  );

  return (
    <Breadcrumb>
      <BreadcrumbList>
        {crumbs.map((crumb, index) => (
          <Fragment key={`${index}:${crumb.label}`}>
            {index > 0 && <BreadcrumbSeparator className="hidden md:block" />}
            <BreadcrumbItem
              className={crumb.href ? "hidden md:block" : undefined}
            >
              {crumb.href ? (
                <BreadcrumbLink asChild>
                  <Link href={crumb.href}>{crumb.label}</Link>
                </BreadcrumbLink>
              ) : (
                <BreadcrumbPage>{crumb.label}</BreadcrumbPage>
              )}
            </BreadcrumbItem>
          </Fragment>
        ))}
      </BreadcrumbList>
    </Breadcrumb>
  );
}
