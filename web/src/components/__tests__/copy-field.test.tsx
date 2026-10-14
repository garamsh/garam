import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { CopyField } from "@/components/copy-field";

describe("CopyField", () => {
  it("shows the value in mono, named by its label", () => {
    render(<CopyField label="Operator ID" value="op-7f3a" />);

    expect(screen.getByText("op-7f3a").tagName).toBe("CODE");
    expect(screen.getByText("op-7f3a")).toHaveAccessibleName("Operator ID");
  });

  it("copies the value and says so", async () => {
    const user = userEvent.setup();
    const writeText = vi
      .spyOn(navigator.clipboard, "writeText")
      .mockResolvedValue();
    render(<CopyField label="Operator ID" value="op-7f3a" />);

    await user.click(screen.getByRole("button", { name: "Copy operator id" }));

    expect(writeText).toHaveBeenCalledWith("op-7f3a");
    expect(await screen.findByText("Copied")).toBeInTheDocument();
  });

  it("says so when the clipboard refuses", async () => {
    const user = userEvent.setup();
    vi.spyOn(navigator.clipboard, "writeText").mockRejectedValue(
      new Error("denied"),
    );
    render(<CopyField label="Operator ID" value="op-7f3a" />);

    await user.click(screen.getByRole("button", { name: "Copy operator id" }));

    // Presence: the sentence goes on to say what to do instead.
    expect(await screen.findByText(/Copy failed/)).toBeInTheDocument();
  });
});
