"use server";

import { redirect } from "next/navigation";
import { z } from "zod";

import {
  GaramApiError,
  GaramUnreachableError,
  garamRequest,
} from "@/lib/garam";
import { PASSWORD_REFUSED } from "@/lib/password-credentials";
import type { components } from "@/types/api";

import { keepSessionCookie } from "./session-cookie";

/** What the sign-in form has to show: nothing yet, or a refusal. */
export type SignInWithPasswordState =
  { status: "idle" } | { status: "error"; message: string };

// Neither field is bounded here. What a username may look like is
// registration's rule, and holding it at sign-in would answer a
// mis-shaped username differently from a wrong password — the
// distinction ADR-0041 exists to keep out of the answer.
const Schema = z.object({ username: z.string(), password: z.string() });

/**
 * Signs in with a username and a password and lands on the console.
 * The password travels in this request's body and nowhere else: no
 * path, no query, and no field of the state this returns.
 */
export async function signInWithPassword(
  _state: SignInWithPasswordState,
  formData: FormData,
): Promise<SignInWithPasswordState> {
  const parsed = Schema.safeParse({
    username: formData.get("username"),
    password: formData.get("password"),
  });
  if (!parsed.success) {
    return { status: "error", message: "The form did not arrive complete." };
  }

  try {
    const response = await garamRequest("/auth/password/login", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(
        parsed.data satisfies components["schemas"]["PasswordLogin"],
      ),
    });
    await keepSessionCookie(response);
  } catch (error) {
    if (error instanceof GaramApiError) {
      // A refused credential renders the console's own sentence rather
      // than the API's, so the one answer the interface gives rests on
      // nothing it cannot check. Every other status keeps the API's
      // message: the bound of ADR-0041 answers 429 here, and reporting
      // that as a wrong password would blame a person for the load.
      return {
        status: "error",
        message: error.status === 401 ? PASSWORD_REFUSED : error.message,
      };
    }
    // An unreachable API is a state the form renders, not a throw: the
    // boundary would replace the form and lose what was typed into it.
    if (error instanceof GaramUnreachableError) {
      return { status: "error", message: error.message };
    }
    throw error;
  }

  redirect("/console");
}
