import "server-only";

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

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
 * The backend never answered: the connection failed, so there is no
 * status to carry. It is a different answer from `GaramApiError`, where
 * the backend replied and refused.
 */
export class GaramUnreachableError extends Error {
  constructor(options?: ErrorOptions) {
    super("the garam API could not be reached", options);
    this.name = "GaramUnreachableError";
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
  if (!response.ok) {
    throw new GaramApiError(response.status, await errorMessage(response));
  }
  return response;
}

/** `garamRequest` for a caller that needs only the body it answered. */
export async function garamFetch<T>(
  path: string,
  init?: RequestInit,
): Promise<T> {
  const response = await garamRequest(path, init);
  if (response.status === 204) {
    return undefined as T;
  }
  return (await response.json()) as T;
}

/**
 * `garamFetch` for data a signed-in page needs. A session the backend
 * rejects sends the visitor to the login page instead of the error
 * boundary: an expired session is a sign-in prompt, not a failure.
 */
export async function garamFetchAuthenticated<T>(
  path: string,
  init?: RequestInit,
): Promise<T> {
  try {
    return await garamFetch<T>(path, init);
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
