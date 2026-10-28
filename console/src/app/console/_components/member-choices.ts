import {
  listGroupMembers,
  listPolicies,
  lookupGroups,
  slugFromGrn,
} from "@/features/orgs";
import {
  MAX_PERMISSION_CHECKS,
  checkPermissions,
  type PermissionCheck,
} from "@/lib/permission-check";

import type { Choice } from "./member-list";

/**
 * What the People and Agents lists both read beside their rows: the
 * teams to filter by, the members of the team filtered on, and the
 * teams and policies a bulk action may name. One permission check asks
 * the page's own `header` actions and every team and policy together;
 * past the operation's limit the rest are offered and left to the
 * server, as an unanswered check is.
 */
export async function readMemberChoices(
  org: string,
  team: string | undefined,
  header: Record<string, PermissionCheck>,
) {
  const [groups, policies, members] = await Promise.all([
    lookupGroups(org),
    listPolicies(org),
    team ? listGroupMembers(org, team) : null,
  ]);
  const teams = (groups.status === "ok" ? groups.data : []).map((group) => ({
    value: slugFromGrn(group.grn),
    label: group.name,
  }));
  const policyList = (policies.status === "ok" ? policies.data : []).map(
    (policy) => ({ value: slugFromGrn(policy.grn), label: policy.name }),
  );
  const pairs: [string, PermissionCheck][] = [
    ...Object.entries(header),
    ...teams.map((choice): [string, PermissionCheck] => [
      `team:${choice.value}`,
      {
        action: "group:member-add",
        resource: { type: "group", id: choice.value },
      },
    ]),
    ...policyList.map((choice): [string, PermissionCheck] => [
      `policy:${choice.value}`,
      {
        action: "policy:attach",
        resource: { type: "policy", id: choice.value },
      },
    ]),
  ];
  const may = await checkPermissions(
    org,
    Object.fromEntries(pairs.slice(0, MAX_PERMISSION_CHECKS)),
  );
  const offered = (prefix: string, choices: Choice[]) =>
    choices.filter(
      (choice) => may.get(`${prefix}:${choice.value}`) !== "denied",
    );
  return {
    may,
    teams,
    teamChoices: offered("team", teams),
    policyChoices: offered("policy", policyList),
    /** Who is in the filtered team; null where none is filtered or unread. */
    inTeam:
      members?.status === "ok"
        ? new Set(members.data.map((member) => member.memberGrn))
        : null,
    /** Whether a team filter was asked and its members could not be read. */
    teamUnread: members !== null && members.status !== "ok",
  };
}
