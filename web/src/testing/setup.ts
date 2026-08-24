import "@testing-library/jest-dom/vitest";

import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, vi } from "vitest";

import { handlers } from "@/testing/mocks/handlers";

/** The mock backend; a test overrides a route with `server.use`. */
export const server = setupServer(...handlers);

// `next/headers` is only wired up inside a request; the tests supply a
// session cookie so the API client behaves as it does when signed in.
vi.mock("next/headers", () => ({
  cookies: () =>
    Promise.resolve({
      get: (name: string) => ({ name, value: "test-session-token" }),
      set: () => {},
      delete: () => {},
    }),
}));

// `server-only` throws when imported outside a React Server Component.
vi.mock("server-only", () => ({}));

// jsdom ships no `matchMedia`; the sidebar's breakpoint hook needs one.
vi.stubGlobal("matchMedia", (query: string) => ({
  matches: false,
  media: query,
  addEventListener: () => {},
  removeEventListener: () => {},
}));

// jsdom implements neither pointer capture nor `scrollIntoView`; the
// Radix menu and select primitives call both while opening.
Element.prototype.hasPointerCapture = () => false;
Element.prototype.setPointerCapture = () => {};
Element.prototype.releasePointerCapture = () => {};
Element.prototype.scrollIntoView = () => {};

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());
