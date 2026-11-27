import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import AbusePage from "@/app/abuse/page";

describe("abuse page", () => {
  it("names the address to report to and what to send", () => {
    render(<AbusePage />);

    expect(
      screen.getByRole("heading", { name: "Report abuse" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "security@garam.sh" }),
    ).toHaveAttribute("href", "mailto:security@garam.sh");
    expect(screen.getByText(/with the full URL/)).toBeInTheDocument();
    expect(screen.getByText(/within 30 days/)).toBeInTheDocument();
  });
});
