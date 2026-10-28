import {
  addGroupMember,
  addProjectMember,
  removeGroupMember,
  removeProjectMember,
} from "@/features/orgs/actions";

/** What holds memberships: a team or a project, by id and name. */
export type MembershipScope = {
  kind: "team" | "project";
  id: string;
  name: string;
};

/**
 * How each kind is named and written to: the noun a sentence uses, the
 * form field the actions read its id from, and the actions themselves.
 */
export const SCOPE_KIND = {
  team: {
    noun: "team",
    plural: "teams",
    field: "group",
    add: addGroupMember,
    remove: removeGroupMember,
  },
  project: {
    noun: "project",
    plural: "projects",
    field: "project",
    add: addProjectMember,
    remove: removeProjectMember,
  },
} as const;
