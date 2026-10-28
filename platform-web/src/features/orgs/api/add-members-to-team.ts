"use server";

import { addGroupMember } from "./add-group-member";

/**
 * What adding several members to one team answered: how many it added,
 * and each it could not, with the reason in the console's words.
 */
export type AddMembersToTeamState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | {
      status: "done";
      added: number;
      refused: { member: string; message: string }[];
    };

/**
 * Adds each member named to one team, one request each, since garam
 * adds one member per request. A refusal for one member does not stop
 * the rest; each is reported by the GRN it named.
 */
export async function addMembersToTeam(
  _state: AddMembersToTeamState,
  formData: FormData,
): Promise<AddMembersToTeamState> {
  const org = formData.get("org");
  const group = formData.get("group");
  const members = formData.getAll("member").map(String);
  if (typeof org !== "string" || typeof group !== "string" || !group) {
    return { status: "error", message: "Choose a team to add them to." };
  }
  if (members.length === 0) {
    return { status: "error", message: "Choose who to add." };
  }

  let added = 0;
  const refused: { member: string; message: string }[] = [];
  for (const member of members) {
    const one = new FormData();
    one.set("org", org);
    one.set("group", group);
    one.set("member", member);
    const answer = await addGroupMember({ status: "idle" }, one);
    if (answer.status === "error") {
      refused.push({ member, message: answer.message });
    } else {
      added += 1;
    }
  }
  return { status: "done", added, refused };
}
