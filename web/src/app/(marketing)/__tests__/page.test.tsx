import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import LandingPage from "@/app/(marketing)/page";

describe("landing page", () => {
  it("introduces the service without asking for a session", () => {
    render(<LandingPage />);

    expect(
      screen.getByRole("heading", {
        name: "One organization for the people and the agents that work in it.",
      }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "What an organization holds" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Invitation, not signup" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "One name shape, five segments" }),
    ).toBeInTheDocument();
  });

  it("frames the agent surface as served but unattended", () => {
    render(<LandingPage />);

    expect(
      screen.getByRole("heading", { name: "Reachable, not yet watched" }),
    ).toBeInTheDocument();
    // Presence: the clause sits mid-sentence in a section body that
    // opens with what is built.
    expect(
      screen.getByText(/the adapter that would run beside the agent/),
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
