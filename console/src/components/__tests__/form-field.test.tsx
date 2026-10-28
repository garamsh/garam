import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { FormField } from "@/components/form-field";
import { Input } from "@/components/ui/input";

describe("FormField", () => {
  it("labels the control and describes it with the hint", () => {
    render(
      <FormField label="Name" hint="Shown to everyone in the organization.">
        {(control) => <Input {...control} />}
      </FormField>,
    );

    const input = screen.getByRole("textbox", { name: "Name" });
    expect(input).toHaveAccessibleDescription(
      "Shown to everyone in the organization.",
    );
    expect(input).not.toHaveAttribute("aria-invalid");
  });

  it("marks the control invalid and ties the error to it", () => {
    render(
      <FormField label="Name" hint="Shown to everyone." error="Enter a name.">
        {(control) => <Input {...control} />}
      </FormField>,
    );

    const input = screen.getByRole("textbox", { name: "Name" });
    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(input).toHaveAccessibleDescription(
      "Shown to everyone. Enter a name.",
    );
    expect(screen.getByRole("alert")).toHaveTextContent("Enter a name.");
  });
});
