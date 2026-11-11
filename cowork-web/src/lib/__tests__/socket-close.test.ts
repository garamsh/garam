import { HttpResponse, http } from "msw";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { advance, startFreshPage } from "@/testing/clock";
import { server } from "@/testing/setup";

import { leaveOnClose } from "../socket-close";

const ORIGIN = "http://cowork.test";

function stubLocation() {
  const location = {
    origin: ORIGIN,
    pathname: "/acme/p1",
    search: "?view=board",
    assign: vi.fn(),
    reload: vi.fn(),
  };
  vi.stubGlobal("location", location);
  return location;
}

/** Answers the tab's question about its session with `respond`. */
function sessionAnswers(respond: () => Response) {
  server.use(http.get(`${ORIGIN}/auth/session`, respond));
}

const SIGN_IN = "/auth/sign-in?next=%2Facme%2Fp1%3Fview%3Dboard";

describe("leaveOnClose", () => {
  beforeEach(startFreshPage);
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it("sends a signed-out visitor to sign in, carrying the page", async () => {
    const location = stubLocation();
    await leaveOnClose(4401);
    expect(location.assign).toHaveBeenCalledWith(SIGN_IN);
    expect(location.reload).not.toHaveBeenCalled();
  });

  it("reloads for a visitor who may no longer see the page", async () => {
    const location = stubLocation();
    await leaveOnClose(4403);
    expect(location.reload).toHaveBeenCalledOnce();
    expect(location.assign).not.toHaveBeenCalled();
  });

  it("sends a visitor to sign in when a failed connection hides a refused session", async () => {
    const location = stubLocation();
    sessionAnswers(() => new HttpResponse(null, { status: 401 }));
    await leaveOnClose(1006);
    expect(location.assign).toHaveBeenCalledWith(SIGN_IN);
  });

  // The control: the same close, with the session standing.
  it.each([
    ["the session stands", () => new HttpResponse(null, { status: 204 })],
    ["the platform fails", () => new HttpResponse(null, { status: 503 })],
    ["the network fails", () => HttpResponse.error()],
  ])("leaves the page be on a failed connection when %s", async (_, answer) => {
    const location = stubLocation();
    sessionAnswers(answer);
    await leaveOnClose(1006);
    expect(location.assign).not.toHaveBeenCalled();
    expect(location.reload).not.toHaveBeenCalled();
  });

  it("asks once for the closes of one interval, and again after it", async () => {
    stubLocation();
    let asked = 0;
    sessionAnswers(() => {
      asked++;
      return new HttpResponse(null, { status: 204 });
    });

    for (let i = 0; i < 12; i++) await leaveOnClose(1006);
    expect(asked).toBe(1);

    advance(30_000);
    await leaveOnClose(1006);
    expect(asked).toBe(2);
  });
});
