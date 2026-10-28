import { z } from "zod";

import {
  GaramUnreachableError,
  garamFetchAuthenticated,
  parserFor,
} from "@/lib/garam";
import type { components } from "@garam/platform-sdk-ts/api";

/** One of the person's sessions, browser or CLI (ADR-0120). */
export type Session = components["schemas"]["Session"];

const SessionParser = parserFor<Session>()(
  z.object({
    id: z.string(),
    kind: z.enum(["browser", "cli"]),
    name: z.string().optional(),
    createdAt: z.string(),
    expiresAt: z.string(),
    current: z.boolean(),
  }),
);

/** The person's sessions, or that garam did not answer. */
export type SessionsRead =
  { status: "ok"; data: Session[] } | { status: "unreachable" };

/**
 * Every live session of the signed-in person, newest first. A session
 * the backend rejects sends them to sign in, as any signed-in read does.
 */
export async function listMySessions(): Promise<SessionsRead> {
  try {
    return {
      status: "ok",
      data: await garamFetchAuthenticated(
        "/me/sessions",
        z.array(SessionParser),
      ),
    };
  } catch (error) {
    if (error instanceof GaramUnreachableError)
      return { status: "unreachable" };
    throw error;
  }
}
