import { afterEach, describe, expect, it, vi } from "vitest";

import { leaveOnRefusal } from "../socket-close";

function stubLocation() {
  const location = {
    pathname: "/acme/p1",
    search: "?view=board",
    assign: vi.fn(),
    reload: vi.fn(),
  };
  vi.stubGlobal("location", location);
  return location;
}

describe("leaveOnRefusal", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("sends a signed-out visitor to sign in, carrying the page", () => {
    const location = stubLocation();
    leaveOnRefusal(4401);
    expect(location.assign).toHaveBeenCalledWith(
      "/auth/sign-in?next=%2Facme%2Fp1%3Fview%3Dboard",
    );
    expect(location.reload).not.toHaveBeenCalled();
  });

  it("reloads for a visitor who may no longer see the page", () => {
    const location = stubLocation();
    leaveOnRefusal(4403);
    expect(location.reload).toHaveBeenCalledOnce();
    expect(location.assign).not.toHaveBeenCalled();
  });

  it.each([1000, 1006, 1013])("leaves the page be on close %i", (code) => {
    const location = stubLocation();
    leaveOnRefusal(code);
    expect(location.assign).not.toHaveBeenCalled();
    expect(location.reload).not.toHaveBeenCalled();
  });
});
