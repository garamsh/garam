import "@testing-library/jest-dom/vitest";

import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, vi } from "vitest";

import { handlers } from "@/testing/mocks/handlers";

export const server = setupServer(...handlers);

// `next/headers` is only wired up inside a request; the tests supply a
// session cookie so the API client behaves as it does when signed in.
vi.mock("next/headers", () => ({
  cookies: () =>
    Promise.resolve({
      get: (name: string) => ({ name, value: "test-session-token" }),
      delete: () => {},
    }),
}));

// `server-only` throws when imported outside a React Server Component.
vi.mock("server-only", () => ({}));

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());
