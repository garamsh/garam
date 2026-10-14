import { describe, expect, it } from "vitest";

import {
  CONTROL_REFUSAL_KINDS,
  CONTROL_REFUSED,
  CONTROL_UNAVAILABLE,
  CONTROL_UNREADABLE,
  MAPPED_CONTROL_KINDS,
  controlFailureCopy,
} from "@/lib/control-refusal-copy";
import type { ControlOperation } from "@/lib/operation-authority";

function refused(
  status: number,
  kind: string | null,
  operation: ControlOperation = "agent-template:read",
) {
  return controlFailureCopy(
    { status: "refused", refusal: { source: "control", status, kind } },
    operation,
  );
}

describe("control refusal kinds", () => {
  it("writes a sentence for every documented kind, and for no other", () => {
    // Read non-empty, so an empty pair cannot pass as agreeing.
    expect(CONTROL_REFUSAL_KINDS.length).toBeGreaterThan(0);

    expect([...MAPPED_CONTROL_KINDS].sort()).toEqual(
      [...CONTROL_REFUSAL_KINDS].sort(),
    );
  });
});

describe("controlFailureCopy", () => {
  it("reads a documented kind as its sentence, never as the status's", () => {
    const sentence = refused(
      400,
      "embedding_required",
      "agent-template:publish",
    );

    expect(sentence).toMatch(/needs an embeddings endpoint/);
    // The control: the same status with no kind reads another sentence.
    expect(refused(400, null, "agent-template:publish")).not.toBe(sentence);
  });

  it("lets the operation that asked supply the sentence for a kind", () => {
    const create = refused(400, "embedding_required", "agent:create");

    expect(create).toMatch(/^This template version names a model/);
    expect(create).not.toBe(
      refused(400, "embedding_required", "agent-template:publish"),
    );
  });

  it("reads an undocumented kind as the fixed sentence, not as its status's", () => {
    expect(refused(409, "recovery_open")).toBe(CONTROL_REFUSED);
    // The control: 409 with no kind has a sentence of its own.
    expect(refused(409, null)).not.toBe(CONTROL_REFUSED);
  });

  it("reads a kind every object answers as undocumented", () => {
    expect(refused(400, "constructor")).toBe(CONTROL_REFUSED);
    expect(refused(400, "toString")).toBe(CONTROL_REFUSED);
  });

  it("reads a status nothing maps as the fixed sentence", () => {
    expect(refused(418, null)).toBe(CONTROL_REFUSED);
    expect(refused(404, null)).not.toBe(CONTROL_REFUSED);
  });

  it("reads the contract kind, an outage and an unreadable answer in the console's words", () => {
    expect(refused(503, "garam_contract_unsupported")).toBe(
      CONTROL_UNAVAILABLE,
    );
    expect(
      controlFailureCopy({ status: "unavailable" }, "agent:execution-read"),
    ).toBe(CONTROL_UNAVAILABLE);
    expect(
      controlFailureCopy(
        { status: "refused", refusal: { source: "unreadable" } },
        "agent:execution-read",
      ),
    ).toBe(CONTROL_UNREADABLE);
  });

  it("keeps the sentence garam's refused mint was already given", () => {
    expect(
      controlFailureCopy(
        {
          status: "refused",
          refusal: { source: "mint", sentence: "You don't have permission." },
        },
        "agent:create",
      ),
    ).toBe("You don't have permission.");
  });
});
