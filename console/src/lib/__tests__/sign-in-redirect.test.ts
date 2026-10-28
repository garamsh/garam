import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";
import { z } from "zod";

import { garamFetchAuthenticated } from "@/lib/garam";
import { RETURN_PATH_HEADER } from "@garam/platform-client/return-path";
import { API_URL } from "@/testing/mocks/handlers";
import { requestHeaders, server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

function sessionRejected() {
  server.use(
    http.get(`${API_URL}/me`, () =>
      HttpResponse.json(
        { kind: "unauthenticated", message: "no session" },
        { status: 401 },
      ),
    ),
  );
}

describe("a session the backend rejects", () => {
  it("sends the visitor to sign in carrying the console path the request was for", async () => {
    sessionRejected();
    requestHeaders.set(RETURN_PATH_HEADER, "/console/invitations/tok-123");

    await expect(garamFetchAuthenticated("/me", z.unknown())).rejects.toThrow(
      /^NEXT_REDIRECT:http:\/\/garam\.localhost:3001\/login\?next=%2Fconsole%2Finvitations%2Ftok-123$/,
    );
  });

  // The control: the same rejection with a path off the console lands
  // on a bare sign-in, so the one above is the path carried and not a
  // redirect that goes wherever the header says.
  it("drops a path that leaves the console", async () => {
    sessionRejected();
    requestHeaders.set(RETURN_PATH_HEADER, "//evil.example/console");

    await expect(garamFetchAuthenticated("/me", z.unknown())).rejects.toThrow(
      /^NEXT_REDIRECT:http:\/\/garam\.localhost:3001\/login$/,
    );
  });
});
