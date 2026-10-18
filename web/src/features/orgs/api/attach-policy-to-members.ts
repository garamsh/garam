"use server";

import { attachPolicy } from "./attach-policy";

/**
 * What attaching one policy to several members answered: how many it
 * attached, and each it could not, with the reason in the console's
 * words.
 */
export type AttachPolicyToMembersState =
  | { status: "idle" }
  | { status: "error"; message: string }
  | {
      status: "done";
      attached: number;
      refused: { member: string; message: string }[];
    };

/**
 * Attaches one policy to each member named, one request each, since
 * garam attaches one subject per request. A refusal for one member
 * does not stop the rest; each is reported by the GRN it named.
 */
export async function attachPolicyToMembers(
  _state: AttachPolicyToMembersState,
  formData: FormData,
): Promise<AttachPolicyToMembersState> {
  const org = formData.get("org");
  const policy = formData.get("policy");
  const members = formData.getAll("member").map(String);
  if (typeof org !== "string" || typeof policy !== "string" || !policy) {
    return { status: "error", message: "Choose a policy to attach." };
  }
  if (members.length === 0) {
    return { status: "error", message: "Choose who to attach it to." };
  }

  let attached = 0;
  const refused: { member: string; message: string }[] = [];
  for (const member of members) {
    const one = new FormData();
    one.set("org", org);
    one.set("policy", policy);
    one.set("subject", member);
    const answer = await attachPolicy({ status: "idle" }, one);
    if (answer.status === "error") {
      refused.push({ member, message: answer.message });
    } else {
      attached += 1;
    }
  }
  return { status: "done", attached, refused };
}
