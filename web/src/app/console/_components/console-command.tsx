import { CommandPalette } from "./command-palette";
import { listConsoleOrgs } from "./console-orgs";

/**
 * Hands the command palette the organizations it offers. An API that
 * did not answer leaves it offering sections and the theme alone.
 */
export async function ConsoleCommand() {
  const orgs = await listConsoleOrgs();
  return <CommandPalette orgs={orgs.status === "ok" ? orgs.orgs : []} />;
}
