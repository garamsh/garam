import "server-only";

import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import { z } from "zod";

import { env } from "@/env";
import { SESSION_COOKIE } from "@/lib/session";

/** A non-2xx response from the backend, carrying its HTTP status. */
export class GaramApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
    this.name = "GaramApiError";
  }
}

/**
 * garam could not serve the read, for a reason no caller of it can act
 * on: either the connection failed, so there is no status to carry, or
 * the backend answered 503 because a dependency it needs — the policy
 * engine, the database, a sign-in provider — is itself unreachable
 * (`api/openapi.yaml` §info). It is a different answer from
 * `GaramApiError`, where the backend reached what it needed and refused.
 */
export class GaramUnreachableError extends Error {
  constructor(options?: ErrorOptions) {
    super("the garam API could not be reached", options);
    this.name = "GaramUnreachableError";
  }
}

/**
 * The backend answered, and what it answered is not what
 * `api/openapi.yaml` says the operation carries. Neither a refusal nor
 * an outage: garam broke a contract this repository generates the
 * client from, so it surfaces as the bug it is rather than as a state a
 * page renders.
 */
export class GaramContractError extends Error {
  constructor(path: string, options?: ErrorOptions) {
    super(`the garam API answered ${path} outside its specification`, options);
    this.name = "GaramContractError";
  }
}

/** True when the error is the backend rejecting an absent session. */
export function isUnauthenticated(error: unknown): boolean {
  return error instanceof GaramApiError && error.status === 401;
}

/**
 * Calls the backend with the caller's session cookie attached and hands
 * back the response it answered with, for a caller that needs more of
 * it than the body — the `Set-Cookie` a sign-in carries.
 *
 * Server-side rendering talks to the backend directly rather than
 * through the `/api/*` rewrite, which exists for the browser.
 */
export async function garamRequest(
  path: string,
  init?: RequestInit,
): Promise<Response> {
  const session = (await cookies()).get(SESSION_COOKIE);
  const response = await fetch(`${env.GARAM_API_URL}${path}`, {
    ...init,
    headers: {
      accept: "application/json",
      ...(session ? { cookie: `${SESSION_COOKIE}=${session.value}` } : {}),
      ...init?.headers,
    },
    cache: "no-store",
  }).catch((cause: unknown) => {
    throw new GaramUnreachableError({ cause });
  });
  // A 503 is an outage rather than a refusal: it says a dependency garam
  // needs is unreachable, and its body names no cause to render
  // (`api/openapi.yaml` §info). Every reading surface already has a state
  // for that, so it arrives as the one the connection failure arrives as.
  if (response.status === 503) {
    throw new GaramUnreachableError();
  }
  if (!response.ok) {
    throw new GaramApiError(response.status, await errorMessage(response));
  }
  return response;
}

/**
 * `garamRequest` for a caller that needs only the body it answered,
 * parsed by `parser`. This is the one place a response body is checked,
 * so nothing downstream checks a field again. A caller that reads none
 * of the body passes `z.unknown()`, which asserts nothing because it
 * uses nothing.
 */
export async function garamFetch<T>(
  path: string,
  parser: z.ZodType<T>,
  init?: RequestInit,
): Promise<T> {
  const response = await garamRequest(path, init);
  // A 204 carries no body, and reading one as JSON rejects.
  const body: unknown =
    response.status === 204
      ? undefined
      : await response.json().catch((cause: unknown) => {
          throw new GaramContractError(path, { cause });
        });
  const parsed = parser.safeParse(body);
  if (!parsed.success) {
    throw new GaramContractError(path, { cause: parsed.error });
  }
  return parsed.data;
}

/**
 * `garamFetch` for data a signed-in page needs. A session the backend
 * rejects sends the visitor to the login page instead of the error
 * boundary: an expired session is a sign-in prompt, not a failure.
 */
export async function garamFetchAuthenticated<T>(
  path: string,
  parser: z.ZodType<T>,
  init?: RequestInit,
): Promise<T> {
  try {
    return await garamFetch(path, parser, init);
  } catch (error) {
    if (isUnauthenticated(error)) redirect("/login");
    throw error;
  }
}

/** Reads the backend's `{ kind, message }` error body, if it sent one. */
async function errorMessage(response: Response): Promise<string> {
  const body: unknown = await response.json().catch(() => null);
  if (
    typeof body === "object" &&
    body !== null &&
    "message" in body &&
    typeof body.message === "string"
  ) {
    return body.message;
  }
  return `${response.status} ${response.statusText}`;
}
