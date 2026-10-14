import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { StageTrack } from "@/components/stage-track";

const stages = [
  { id: "frozen", label: "Frozen" },
  { id: "switched", label: "Switched" },
  { id: "done", label: "Done" },
];

describe("StageTrack", () => {
  it("marks the stage the lifecycle is in", () => {
    render(<StageTrack label="Cutover" stages={stages} current="switched" />);

    const track = screen.getByRole("list", { name: "Cutover" });
    const items = within(track).getAllByRole("listitem");
    expect(items[1]).toHaveAttribute("aria-current", "step");
    expect(items[0]).not.toHaveAttribute("aria-current");
  });

  it("says each stage's state in words", () => {
    render(<StageTrack label="Cutover" stages={stages} current="switched" />);

    const items = screen.getAllByRole("listitem");
    expect(items[0]).toHaveTextContent("Frozen, completed");
    expect(items[1]).toHaveTextContent("Switched, current stage");
    expect(items[2]).toHaveTextContent("Done, not reached");
  });

  it("says where a failed lifecycle stopped", () => {
    render(
      <StageTrack label="Recovery" stages={stages} current="switched" failed />,
    );

    expect(screen.getAllByRole("listitem")[1]).toHaveTextContent(
      "Switched, failed",
    );
  });
});
