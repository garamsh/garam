import { listConsoleOrgs } from "./console-orgs";
import { RouteBreadcrumb } from "./route-breadcrumb";

/** Hands the breadcrumb the names it labels organization slugs with. */
export async function ConsoleBreadcrumb() {
  return <RouteBreadcrumb orgs={await listConsoleOrgs()} />;
}
