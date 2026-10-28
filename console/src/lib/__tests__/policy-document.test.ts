import { describe, expect, it } from "vitest";

import {
  hasCondition,
  parsePolicyDocument,
  placeProblem,
  readStatement,
  statementsOf,
  writeStatement,
} from "@/lib/policy-document";

describe("parsePolicyDocument", () => {
  it.each([
    ["text that is not JSON", "{ Version:", "The document is not valid JSON."],
    ["a JSON array", "[]", "The document must be a JSON object."],
    ["JSON null", "null", "The document must be a JSON object."],
    ["a JSON string", '"Deny"', "The document must be a JSON object."],
  ])("refuses %s, and takes the object beside it", (_, text, message) => {
    expect(parsePolicyDocument(text)).toEqual({ status: "error", message });
    // The control: an object is taken whole, keys the console does not
    // know included, since the engine is what judges them.
    expect(parsePolicyDocument('{"Version":"2026-07-25","X":1}')).toEqual({
      status: "ok",
      value: { Version: "2026-07-25", X: 1 },
    });
  });
});

describe("hasCondition", () => {
  it("finds a Condition on any statement, and none where there is none", () => {
    const statement = { Effect: "Deny", Action: ["x:y"], Resource: ["*"] };
    expect(
      hasCondition({
        Statement: [statement, { ...statement, Condition: { a: 1 } }],
      }),
    ).toBe(true);
    // The control: the same statements without the key.
    expect(hasCondition({ Statement: [statement, statement] })).toBe(false);
    expect(hasCondition({ Statement: "not a list" })).toBe(false);
  });
});

describe("placeProblem", () => {
  it.each([
    ["/Statement/1/Effect", { at: "statement", index: 1, key: "Effect" }],
    // Below a list: the list's control.
    ["/Statement/0/Action/2", { at: "statement", index: 0, key: "Action" }],
    // A key the view has no control for: the statement.
    ["/Statement/0/Principal", { at: "statement", index: 0 }],
    ["/Statement/2", { at: "statement", index: 2 }],
    ["/Version", { at: "version" }],
    ["", { at: "document" }],
    ["/Other", { at: "document" }],
    ["/Statement/x/Effect", { at: "document" }],
  ])("places %j", (path, place) => {
    expect(placeProblem(path)).toEqual(place);
  });

  it("places a refusal with no pointer at the document", () => {
    expect(placeProblem(undefined)).toEqual({ at: "document" });
  });
});

describe("readStatement and writeStatement", () => {
  it("keeps keys it does not edit, Condition among them", () => {
    const statement = {
      Effect: "Deny",
      Action: ["project:delete"],
      Resource: ["*"],
      Condition: { a: 1 },
    };

    expect(readStatement(statement)).toEqual({
      sid: "",
      effect: "Deny",
      actions: ["project:delete"],
      resources: ["*"],
      conditioned: true,
    });
    expect(
      writeStatement(statement, {
        sid: "",
        effect: "Allow",
        actions: ["project:read"],
        resources: ["*"],
      }),
    ).toEqual({
      Effect: "Allow",
      Action: ["project:read"],
      Resource: ["*"],
      Condition: { a: 1 },
    });
  });

  it("leaves an Action that is not a list to the JSON view, as the engine refuses it", () => {
    // garam refuses a string Action as policy_document_wrong_type, so
    // the view does not turn one into a list behind the author's back.
    expect(
      readStatement({ Effect: "Deny", Action: "project:delete", Resource: [] }),
    ).toBeNull();
    expect(
      readStatement({ Effect: "Deny", Action: [], Resource: ["*", 1] }),
    ).toBeNull();
    // The control: the same statement with a list is read.
    expect(
      readStatement({
        Effect: "Deny",
        Action: ["project:delete"],
        Resource: [],
      }),
    ).not.toBeNull();
  });

  it("reads keys as the engine does, without regard to case, and writes over them", () => {
    const statement = { effect: "Deny", action: ["project:delete"] };

    expect(readStatement(statement)).toMatchObject({
      effect: "Deny",
      actions: ["project:delete"],
    });
    // An edit writes under the key already there, adding no second
    // spelling the engine would read as a duplicate.
    expect(
      writeStatement(statement, {
        sid: "",
        effect: "Allow",
        actions: ["project:read"],
        resources: ["*"],
      }),
    ).toEqual({
      effect: "Allow",
      action: ["project:read"],
      Resource: ["*"],
    });
    expect(statementsOf({ statement: [1] })).toEqual([1]);
    expect(hasCondition({ statement: [{ condition: {} }] })).toBe(true);
    // The control: an exact spelling wins where both are written.
    expect(readStatement({ Effect: "Allow", effect: "Deny" })?.effect).toBe(
      "Allow",
    );
  });

  it("adds a Sid only once one is given, and keeps one that was there", () => {
    const fields = { sid: "", effect: "Deny", actions: [], resources: [] };
    expect(writeStatement({}, fields)).not.toHaveProperty("Sid");
    // The control: a Sid already present is written back, empty or not.
    expect(writeStatement({ Sid: "x" }, fields)).toHaveProperty("Sid", "");
    expect(writeStatement({}, { ...fields, sid: "s1" })).toHaveProperty(
      "Sid",
      "s1",
    );
  });

  it("reads no fields from what is not an object", () => {
    expect(readStatement([])).toBeNull();
    expect(readStatement("Deny")).toBeNull();
    expect(readStatement({})).not.toBeNull();
  });

  it("says a Statement that is not a list cannot be shown as statements", () => {
    expect(statementsOf({ Statement: {} })).toBeNull();
    expect(statementsOf({})).toEqual([]);
    expect(statementsOf({ Statement: [1] })).toEqual([1]);
  });
});
