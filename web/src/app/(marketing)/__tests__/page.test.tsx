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
    expect(
      screen.getByRole("heading", { name: "Organizations and invitations" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Every request ends in a decision" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "One name shape, five segments" }),
    ).toBeInTheDocument();
  });

  it("offers the console and the login page as the ways in", () => {
    render(<LandingPage />);

    for (const link of screen.getAllByRole("link", {
      name: "Open the console",
    })) {
      expect(link).toHaveAttribute("href", "/console");
    }
    expect(screen.getByRole("link", { name: "Sign in" })).toHaveAttribute(
      "href",
      "/login",
    );
  });
});
