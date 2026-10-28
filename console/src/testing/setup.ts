import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, vi } from "vitest";

import { handlers } from "@/testing/mocks/handlers";

/** The mock backend; a test overrides a route with `server.use`. */
export const server = setupServer(...handlers);

/**
 * The request headers `headers()` answers with. Empty unless a test
 * sets one, and emptied after each.
 */
export const requestHeaders = new Headers();

// `next/headers` is only wired up inside a request; the tests supply a
// session cookie so the API client behaves as it does when signed in.
vi.mock("next/headers", () => ({
  headers: () => Promise.resolve(requestHeaders),
  cookies: () =>
    Promise.resolve({
      get: (name: string) => ({ name, value: "test-session-token" }),
      set: () => {},
      delete: () => {},
    }),
}));

// `server-only` throws when imported outside a React Server Component.
vi.mock("server-only", () => ({}));

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => {
  server.resetHandlers();
  for (const name of [...requestHeaders.keys()]) requestHeaders.delete(name);
});
afterAll(() => server.close());
