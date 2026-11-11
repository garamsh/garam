import "@testing-library/jest-dom/vitest";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll } from "vitest";

import { handlers } from "@/testing/mocks/handlers";

/** The mock platform; a test overrides a route with `server.use`. */
export const server = setupServer(...handlers);

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());
