import { listConsoleOrgs } from "./console-orgs";
import { RouteBreadcrumb } from "./route-breadcrumb";

/**
 * Hands the breadcrumb the names it labels organization slugs with. An
 * API that did not answer leaves the trail on slugs rather than throwing
 * out of the layout, which has no boundary of its own.
 */
export async function ConsoleBreadcrumb() {
  const orgs = await listConsoleOrgs();
  return <RouteBreadcrumb orgs={orgs.status === "ok" ? orgs.orgs : []} />;
}
