"use server";

import { redirect } from "next/navigation";
import { z } from "zod";

import { joinedOrgPath } from "@/features/orgs";
import { garamRequest, refusal } from "@/lib/garam";
import { parsePassword, parseUsername } from "@/lib/password-credentials";
import type { components } from "@/types/api";

import { keepSessionCookie } from "./session-cookie";

/** What the register form has to show: nothing yet, or a refusal. */
export type RegisterWithPasswordState =
  { status: "idle" } | { status: "error"; message: string };

const Schema = z.object({
  invitationToken: z.string().trim(),
  username: z.string(),
  password: z.string(),
});

/**
 * Creates the account an invitation names, signs it in, and lands in
 * the organization the invitation joined it to. There is no
 * registration without an invitation: organizations grow by invitation
 * rather than by signup (ADR-0006), and the token is what authorizes
 * the account (ADR-0041). The password travels in this request's body
 * and nowhere else.
 */
export async function registerWithPassword(
  _state: RegisterWithPasswordState,
  formData: FormData,
): Promise<RegisterWithPasswordState> {
  const parsed = Schema.safeParse({
    invitationToken: formData.get("invitationToken"),
    username: formData.get("username"),
    password: formData.get("password"),
  });
  if (!parsed.success) {
    return { status: "error", message: "The form did not arrive complete." };
  }
  if (parsed.data.invitationToken === "") {
    return {
      status: "error",
      message: "Paste the invitation token you were sent.",
    };
  }
  const username = parseUsername(parsed.data.username);
  if (username.status === "error") return username;
  const password = parsePassword(parsed.data.password);
  if (password.status === "error") return password;

  try {
    const response = await garamRequest("/auth/password/register", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        invitationToken: parsed.data.invitationToken,
        username: username.value,
        password: password.value,
      } satisfies components["schemas"]["PasswordRegistration"]),
    });
    await keepSessionCookie(response);
  } catch (error) {
    // An unreachable API is a state the form renders, not a throw: the
    // boundary would replace the form and lose what was typed into it.
    return refusal(error, "registerPassword");
  }

  redirect(await joinedOrgPath(parsed.data.invitationToken));
}
