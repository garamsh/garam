import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import LandingPage from "@/app/(marketing)/page";

describe("landing page", () => {
  it("introduces the service without asking for a session", () => {
    render(<LandingPage />);

    expect(
      screen.getByRole("heading", {
        name: "Who belongs to your organization, and what they may do.",
      }),
    ).toBeInTheDocument();
    expect(screen.getByText("Organizations and members")).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "One identifier for everything" }),
    ).toBeInTheDocument();
  });

  it("offers the console as the way in", () => {
    render(<LandingPage />);

    for (const link of screen.getAllByRole("link", {
      name: "Open the console",
    })) {
      expect(link).toHaveAttribute("href", "/console");
    }
  });
});
