import { describe, expect, it } from "vitest";

import { AGENT_NAME_MAX_LENGTH } from "@/lib/agent-definition";
import {
  TEMPLATE_DESCRIPTION_MAX_LENGTH,
  parseTemplateFields,
} from "@/lib/agent-template";

describe("parseTemplateFields", () => {
  it("trims the name and description and reads the values", () => {
    expect(
      parseTemplateFields("  Researcher ", " Reads papers ", [
        { key: "model", value: "haiku" },
        { key: "", value: "" },
      ]),
    ).toEqual({
      status: "ok",
      value: {
        name: "Researcher",
        description: "Reads papers",
        values: { model: "haiku" },
      },
    });
  });

  it("refuses a name that is only whitespace", () => {
    expect(parseTemplateFields("   ", "", [])).toEqual({
      status: "error",
      message: "Give the template a name.",
    });
  });

  it("counts characters as garam does, not UTF-16 units", () => {
    // Each of these is one character and two UTF-16 units, so a count
    // of units would refuse the longest name garam accepts.
    const longest = "𝒜".repeat(AGENT_NAME_MAX_LENGTH);
    expect(parseTemplateFields(longest, "", []).status).toBe("ok");
    expect(parseTemplateFields(`${longest}a`, "", [])).toEqual({
      status: "error",
      message: `A name holds at most ${AGENT_NAME_MAX_LENGTH} characters.`,
    });
  });

  it("bounds the description", () => {
    const at = "d".repeat(TEMPLATE_DESCRIPTION_MAX_LENGTH);
    expect(parseTemplateFields("T", at, []).status).toBe("ok");
    expect(parseTemplateFields("T", `${at}d`, []).status).toBe("error");
  });

  it("refuses values a definition could not carry", () => {
    expect(
      parseTemplateFields("T", "", [{ key: "Model", value: "haiku" }]).status,
    ).toBe("error");
  });
});
