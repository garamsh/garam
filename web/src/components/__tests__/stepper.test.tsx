import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { Stepper } from "@/components/stepper";

const steps = [
  { id: "register", title: "Register the operator" },
  { id: "keys", title: "Issue keys" },
  { id: "verify", title: "Verify the first exchange" },
];

describe("Stepper", () => {
  it("marks the step in hand", () => {
    render(
      <Stepper
        label="Onboarding"
        steps={steps}
        current="keys"
        completed={["register"]}
      />,
    );

    const items = screen.getAllByRole("listitem");
    expect(items[1]).toHaveAttribute("aria-current", "step");
    expect(items[1]).toHaveTextContent("Issue keys");
  });

  it("says which steps are done, so a resumed wizard shows its progress", () => {
    render(
      <Stepper
        label="Onboarding"
        steps={steps}
        current="verify"
        completed={["register", "keys"]}
      />,
    );

    const items = screen.getAllByRole("listitem");
    expect(items[0]).toHaveTextContent("Register the operator (done)");
    expect(items[1]).toHaveTextContent("Issue keys (done)");
    // Control: the step in hand is not done yet.
    expect(items[2]).not.toHaveTextContent("(done)");
  });
});
