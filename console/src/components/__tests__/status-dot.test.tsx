import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { StatusDot } from "@/components/status-dot";

describe("StatusDot", () => {
  it("says the status in words beside the dot", () => {
    render(<StatusDot tone="danger" label="Disconnected" />);

    expect(screen.getByText("Disconnected")).toHaveAttribute(
      "data-tone",
      "danger",
    );
  });

  it("keeps the dot out of the accessible text", () => {
    const { container } = render(<StatusDot tone="ok" label="Connected" />);

    expect(container.querySelector("[aria-hidden]")).toBeInTheDocument();
    expect(container.textContent).toBe("Connected");
  });
});
