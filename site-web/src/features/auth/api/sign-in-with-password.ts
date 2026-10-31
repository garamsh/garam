"use server";

import { redirect } from "next/navigation";
import { z } from "zod";

import { GaramApiError, garamRequest, refusal } from "@/lib/garam";
import { PASSWORD_REFUSED } from "@garam/platform-sdk-ts/password-credentials";
import { APP_PARAM, handoffAppOf } from "@garam/platform-sdk-ts/handoff";
import {
  DEFAULT_RETURN_PATH,
  RETURN_PARAM,
  safeReturnPath,
} from "@garam/platform-sdk-ts/return-path";
import type { components } from "@garam/platform-sdk-ts/api";

import { keepSessionCookie } from "@garam/platform-sdk-ts/session-cookie";
import { appUrl } from "@/lib/app-url";

/** What the sign-in form has to show: nothing yet, or a refusal. */
export type SignInWithPasswordState =
  { status: "idle" } | { status: "error"; message: string };

// Neither field is bounded here. What a username may look like is
// registration's rule, and holding it at sign-in would answer a
// mis-shaped username differently from a wrong password — the
// distinction ADR-0041 exists to keep out of the answer.
const Schema = z.object({ username: z.string(), password: z.string() });

/**
 * Signs in with a username and a password and lands where the visitor
 * was going — the app and the return path the form carries — or on the
 * console.
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
  const app = handoffAppOf(formData.get(APP_PARAM));
  if (!parsed.success || app === null) {
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
    // A refused credential renders the console's own sentence, so the
    // one answer the interface gives rests on nothing it cannot check.
    // Every other refusal reads as `refusal` writes it: the bound of
    // ADR-0041 answers 429 here, and reporting that as a wrong password
    // would blame a person for the load. An unreachable API is a state
    // the form renders, not a throw: the boundary would replace the
    // form and lose what was typed into it.
    if (error instanceof GaramApiError && error.status === 401) {
      return { status: "error", message: PASSWORD_REFUSED };
    }
    return refusal(error);
  }

  redirect(
    appUrl(
      app,
      safeReturnPath(formData.get(RETURN_PARAM), app) ??
        DEFAULT_RETURN_PATH[app],
    ),
  );
}
