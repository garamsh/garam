import { HttpResponse, http } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, vi } from "vitest";

/** The backend address the tests bind a client to. */
export const API_URL = "http://localhost:8080";

/** The user `/me` answers with unless a test overrides it. */
export const alice = { grn: "grn:root:default:user:alice", nickname: "Alice" };

/** The mock backend; a test overrides a route with `server.use`. */
export const server = setupServer(
  http.get(`${API_URL}/me`, () => HttpResponse.json(alice)),
);

// `next/headers` is only wired up inside a request; the tests supply a
// session cookie so the client behaves as it does when signed in.
vi.mock("next/headers", () => ({
  cookies: () =>
    Promise.resolve({
      get: (name: string) => ({ name, value: "test-session-token" }),
    }),
}));

// `server-only` throws when imported outside a React Server Component.
vi.mock("server-only", () => ({}));

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());
