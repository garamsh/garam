import "server-only";

import { cookies } from "next/headers";
import { z } from "zod";

import {
  UNREACHABLE_COPY,
  refusalCopy,
  type RefusalOperation,
} from "./refusal-copy";
import { SESSION_COOKIE } from "./session";
import type { components } from "./api";

/**
 * A non-2xx response from the backend, carrying its HTTP status and the
 * `kind` its error body named (`platform/api/openapi.yaml` `Error`).
 * The two are what a refusal is told apart by; `message` is the
 * server's English, which a person reads only through `refusal`.
 */
export class GaramApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
    readonly kind: string = "unknown",
    readonly reason?: { code: string; path?: string },
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
 * (`platform/api/openapi.yaml` §info). It is a different answer from
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
 * `platform/api/openapi.yaml` says the operation carries. Neither a
 * refusal nor an outage: garam broke a contract this repository
 * generates the client from, so it surfaces as the bug it is rather
 * than as a state a page renders.
 */
export class GaramContractError extends Error {
  constructor(path: string, options?: ErrorOptions) {
    super(`the garam API answered ${path} outside its specification`, options);
    this.name = "GaramContractError";
  }
}

/**
 * The parser `garamFetch` takes for a body, tied to the type
 * `platform/api/openapi.yaml` generated in both directions: it produces
 * `T`, and it accepts every body `T` describes. A `z.ZodType<T>`
 * annotation carries only the first — a parser demanding a key the
 * specification does not describe still produces `T`, so it typechecks
 * and then refuses a valid response as a contract violation (#896).
 *
 * Written `parserFor<Operator>()(z.object({ ... }))`: `T` is named so
 * that the parser stays inferred and what it demands can be read off it.
 */
export function parserFor<T>() {
  return <S extends z.ZodType<T>>(
    parser: S &
      (T extends z.input<S>
        ? unknown
        : {
            "requires what platform/api/openapi.yaml does not describe": never;
          }),
  ): z.ZodType<T> => parser;
}

/**
 * What a Server Action returns when garam refused it or could not be
 * reached: the expected failures, which the form renders beside what
 * was typed rather than a boundary replacing it
 * (`docs/convention/stack-nextjs.md` §Errors — returned, thrown, or
 * caught). Anything else is a bug and is
 * thrown on, so `return refusal(error)` is a catch clause's last line.
 *
 * The message is the sentence `refusal-copy.ts` writes for the
 * refusal's status and kind, read for `operation` where the same pair
 * means something particular to it. The server's own text reaches a
 * person only inside the generic sentence for a refusal nothing maps.
 */
export function refusal(
  error: unknown,
  operation?: RefusalOperation,
): { status: "error"; message: string } {
  if (error instanceof GaramApiError) {
    return {
      status: "error",
      message: refusalCopy(
        {
          status: error.status,
          kind: error.kind,
          message: error.message,
          reason: error.reason?.code,
          path: error.reason?.path,
        },
        operation,
      ),
    };
  }
  if (error instanceof GaramUnreachableError) {
    return { status: "error", message: UNREACHABLE_COPY };
  }
  throw error;
}

/** True when the error is the backend rejecting an absent session. */
export function isUnauthenticated(error: unknown): boolean {
  return error instanceof GaramApiError && error.status === 401;
}

/** What an app binds its client of the platform to. */
export type GaramClientConfig = {
  /** The backend's address, which server-side rendering calls directly. */
  apiUrl: string;
  /**
   * Sends a visitor whose session the backend rejected to sign in. Each
   * app knows where its sign-in is, so each supplies it.
   */
  redirectToSignIn: () => Promise<never>;
};

/**
 * The three calls an app makes to the platform, bound to its backend
 * address and its sign-in. Each app binds them once, in its own
 * `lib/garam.ts`.
 */
export function garamClient(config: GaramClientConfig) {
  /**
   * Calls the backend with the caller's session cookie attached and hands
   * back the response it answered with, for a caller that needs more of
   * it than the body — the `Set-Cookie` a sign-in carries.
   *
   * Server-side rendering talks to the backend directly rather than
   * through the `/api/*` rewrite, which exists for the browser.
   */
  async function garamRequest(
    path: string,
    init?: RequestInit,
  ): Promise<Response> {
    const session = (await cookies()).get(SESSION_COOKIE);
    const response = await fetch(`${config.apiUrl}${path}`, {
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
    // A 503 is an outage rather than a refusal: it says a dependency
    // garam needs is unreachable, and its body names no cause to
    // render (`platform/api/openapi.yaml` §info). Every reading surface
    // already has a state for that, so it arrives as the one the
    // connection failure arrives as.
    if (response.status === 503) {
      throw new GaramUnreachableError();
    }
    if (!response.ok) {
      const { message, kind, reason, path } = await errorBody(response);
      throw new GaramApiError(
        response.status,
        message,
        kind,
        reason === undefined ? undefined : { code: reason, path },
      );
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
  async function garamFetch<T>(
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
   * rejects sends the visitor to the sign-in page instead of the error
   * boundary: an expired session is a sign-in prompt, not a failure.
   */
  async function garamFetchAuthenticated<T>(
    path: string,
    parser: z.ZodType<T>,
    init?: RequestInit,
  ): Promise<T> {
    try {
      return await garamFetch(path, parser, init);
    } catch (error) {
      if (isUnauthenticated(error)) await config.redirectToSignIn();
      throw error;
    }
  }

  return { garamRequest, garamFetch, garamFetchAuthenticated };
}

const ErrorBodyParser = parserFor<components["schemas"]["Error"]>()(
  z.object({
    kind: z.string(),
    message: z.string(),
    reason: z.string().optional(),
    path: z.string().optional(),
  }),
);

/**
 * Reads the backend's `{ kind, message }` error body. An answer without
 * one — a proxy's page in front of garam — keeps its status, with the
 * kind unknown and the status line as its text.
 */
async function errorBody(
  response: Response,
): Promise<components["schemas"]["Error"]> {
  const parsed = ErrorBodyParser.safeParse(
    await response.json().catch(() => null),
  );
  if (parsed.success) return parsed.data;
  return {
    kind: "unknown",
    message: `${response.status} ${response.statusText}`,
  };
}
