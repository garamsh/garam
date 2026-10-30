import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { GoogleSignInButton } from "@/features/auth/components/google-sign-in-button";

// Google's branding guidelines require its own button asset, an
// approved label, and both theme variants:
// https://developers.google.com/identity/branding-guidelines
describe("Google sign-in button", () => {
  it("posts to the backend OAuth entry point under the approved label", () => {
    render(<GoogleSignInButton />);

    const button = screen.getByRole("button", { name: "Sign in with Google" });
    const form = button.closest("form");
    expect(form).toHaveAttribute("method", "post");
    expect(form).toHaveAttribute("action", "/api/auth/login/google");
    expect(form?.querySelector('input[name="next"]')).toBeNull();
  });

  it("carries the return path in the form body, not the URL", () => {
    render(<GoogleSignInButton returnPath="/console/invitations/tok-1" />);

    const form = screen
      .getByRole("button", { name: "Sign in with Google" })
      .closest("form");
    expect(form).toHaveAttribute("action", "/api/auth/login/google");
    expect(
      form?.querySelector('input[type="hidden"][name="next"]'),
    ).toHaveValue("/console/invitations/tok-1");
  });

  it("names the app the sign-in is for in the form body", () => {
    render(<GoogleSignInButton app="cowork" returnPath="/acme" />);

    const form = screen
      .getByRole("button", { name: "Sign in with Google" })
      .closest("form");
    expect(form?.querySelector('input[type="hidden"][name="app"]')).toHaveValue(
      "cowork",
    );
  });

  it("renders Google's own button asset for the light and dark themes", () => {
    const { container } = render(<GoogleSignInButton />);

    const sources = [...container.querySelectorAll("img")].map((image) =>
      decodeURIComponent(image.getAttribute("src") ?? ""),
    );

    expect(sources).toHaveLength(2);
    expect(sources[0]).toContain("/google/sign-in-with-google-light.png");
    expect(sources[1]).toContain("/google/sign-in-with-google-dark.png");
  });
});
