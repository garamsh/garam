import { describe, expect, it } from "vitest";

import {
  PASSWORD_MAX_BYTES,
  PASSWORD_MIN_LENGTH,
  USERNAME_MAX_LENGTH,
  parsePassword,
  parseUsername,
} from "@/lib/password-credentials";

describe("parseUsername", () => {
  it("takes lowercase letters, digits and hyphens", () => {
    expect(parseUsername(" alice-2 ")).toEqual({
      status: "ok",
      value: "alice-2",
    });
  });

  it("refuses what the character rule does not allow", () => {
    expect(parseUsername("Alice")).toEqual({
      status: "error",
      message: "A username holds only lowercase letters, digits and hyphens.",
    });

    // The control: the same username cased down is taken, so the
    // character rule is what refused it.
    expect(parseUsername("alice")).toEqual({ status: "ok", value: "alice" });
  });

  it("refuses one character past the length the reserved prefix leaves", () => {
    const longest = "a".repeat(USERNAME_MAX_LENGTH);

    expect(parseUsername(longest)).toEqual({ status: "ok", value: longest });
    expect(parseUsername(`${longest}a`)).toEqual({
      status: "error",
      message: `A username holds at most ${USERNAME_MAX_LENGTH} characters; this one holds ${USERNAME_MAX_LENGTH + 1}.`,
    });
  });

  it("asks for a username that arrived blank", () => {
    expect(parseUsername("   ")).toEqual({
      status: "error",
      message: "Choose the username the account is to have.",
    });
  });
});

describe("parsePassword", () => {
  it("refuses one character short of the floor", () => {
    expect(parsePassword("a".repeat(PASSWORD_MIN_LENGTH - 1))).toEqual({
      status: "error",
      message: `A password holds at least ${PASSWORD_MIN_LENGTH} characters.`,
    });

    // The control: one character more is taken, so the floor is what
    // refused it.
    const floor = "a".repeat(PASSWORD_MIN_LENGTH);
    expect(parsePassword(floor)).toEqual({ status: "ok", value: floor });
  });

  it("counts the floor in characters rather than in UTF-16 units", () => {
    // Eight astral characters are sixteen UTF-16 units, and a count
    // taken from the string's length would pass a password of eight.
    const eight = "\u{1D11E}".repeat(8);
    expect(eight.length).toBe(16);

    expect(parsePassword(eight)).toEqual({
      status: "error",
      message: `A password holds at least ${PASSWORD_MIN_LENGTH} characters.`,
    });
  });

  it("counts the ceiling in bytes rather than in characters", () => {
    // Each of these is four UTF-8 bytes, so thirty-two of them sit on
    // the ceiling while the character count is a quarter of it.
    const atCeiling = "\u{1D11E}".repeat(PASSWORD_MAX_BYTES / 4);

    expect(parsePassword(atCeiling)).toEqual({
      status: "ok",
      value: atCeiling,
    });
    expect(parsePassword(`${atCeiling}\u{1D11E}`)).toEqual({
      status: "error",
      message: `A password holds at most ${PASSWORD_MAX_BYTES} bytes.`,
    });
  });

  it("keeps the spaces a person chose", () => {
    const padded = `  ${"a".repeat(PASSWORD_MIN_LENGTH)}  `;

    expect(parsePassword(padded)).toEqual({ status: "ok", value: padded });
  });

  it("names the bound and never what was typed", () => {
    // The first half of the name is the property: a refusal that lost
    // its message carries no password either.
    expect(parsePassword("hunter2")).toEqual({
      status: "error",
      message: `A password holds at least ${PASSWORD_MIN_LENGTH} characters.`,
    });
    expect(JSON.stringify(parsePassword("hunter2"))).not.toContain("hunter2");
  });
});
