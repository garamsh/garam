import { describe, expect, it } from "vitest";

import { handoffAppOf } from "../handoff";

describe("handoffAppOf", () => {
  it("reads an app the site hands a sign-in to", () => {
    expect(handoffAppOf("console")).toBe("console");
    expect(handoffAppOf("cowork")).toBe("cowork");
  });

  it("reads the console where the request names no app", () => {
    expect(handoffAppOf(null)).toBe("console");
    expect(handoffAppOf(undefined)).toBe("console");
  });

  it.each(["evil", "", "Cowork", "https://cowork.garam.sh"])(
    "refuses %j rather than reading it as another app",
    (value) => {
      expect(handoffAppOf(value)).toBeNull();
    },
  );
});
