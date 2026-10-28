import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { Banner } from "../banner";

describe("Banner", () => {
  it("announces a danger banner as an alert", () => {
    render(
      <Banner tone="danger" title="Two agents are failing">
        Their operator stopped reporting.
      </Banner>,
    );

    const banner = screen.getByRole("alert");
    expect(banner).toHaveTextContent("Two agents are failing");
    expect(banner).toHaveTextContent("Their operator stopped reporting.");
  });

  it("announces any other tone politely", () => {
    render(<Banner tone="warn" title="Consents were not checked" />);

    expect(screen.getByRole("status")).toHaveTextContent(
      "Consents were not checked",
    );
    // Control: the same banner raised as danger is the alert above.
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("carries the action that resolves it", () => {
    render(
      <Banner
        tone="info"
        title="An operator has no control endpoint"
        action={<button type="button">Register endpoint</button>}
      />,
    );

    expect(
      screen.getByRole("button", { name: "Register endpoint" }),
    ).toBeInTheDocument();
  });
});
