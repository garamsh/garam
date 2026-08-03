import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { GoogleSignInButton } from "@/features/auth/components/google-sign-in-button";

// Google's branding guidelines require its own button asset, an
// approved label, and both theme variants:
// https://developers.google.com/identity/branding-guidelines
describe("Google sign-in button", () => {
  it("links to the backend OAuth entry point under the approved label", () => {
    render(<GoogleSignInButton />);

    expect(
      screen.getByRole("link", { name: "Sign in with Google" }),
    ).toHaveAttribute("href", "/api/auth/login/google");
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
