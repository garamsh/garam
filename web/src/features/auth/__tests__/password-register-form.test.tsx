import { render, screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { PasswordRegisterForm } from "@/features/auth/components/password-register-form";
import {
  PASSWORD_MIN_LENGTH,
  USERNAME_MAX_LENGTH,
} from "@/lib/password-credentials";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";
import { setupUser } from "@/testing/user";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

/** The password these tests register with; it is a fixture, not a value. */
const PASSWORD = "correct-horse-battery-staple";

const TOKEN = "invitation-token-nobody-guesses";

/** Registers through the form and answers with what it then shows. */
async function register(token: string, username: string, password: string) {
  const user = setupUser();
  render(<PasswordRegisterForm />);

  await user.type(screen.getByLabelText("Invitation token"), token);
  await user.type(screen.getByLabelText("Username"), username);
  await user.type(screen.getByLabelText("Password"), password);
  await user.click(screen.getByRole("button", { name: "Create account" }));

  return (await screen.findByRole("alert")).textContent;
}

describe("password register form", () => {
  it("sends the invitation and the credential in the body of a POST, and the password in no URL", async () => {
    const seen: { method: string; url: string; body: unknown }[] = [];
    server.use(
      http.post(`${API_URL}/auth/password/register`, async ({ request }) => {
        seen.push({
          method: request.method,
          url: request.url,
          body: await request.json(),
        });
        return HttpResponse.json(
          { kind: "not_found", message: "invitation not found" },
          { status: 404 },
        );
      }),
    );

    const refusal = await register(TOKEN, "alice", PASSWORD);

    expect(seen).toEqual([
      {
        method: "POST",
        url: `${API_URL}/auth/password/register`,
        body: {
          invitationToken: TOKEN,
          username: "alice",
          password: PASSWORD,
        },
      },
    ]);
    expect(refusal).toBe("invitation not found");
    expect(document.body.innerHTML).not.toContain(PASSWORD);
  });

  it("keeps the pasted invitation when the API refuses", async () => {
    server.use(
      http.post(`${API_URL}/auth/password/register`, () =>
        HttpResponse.json(
          { kind: "already_exists", message: "user: already exists" },
          { status: 409 },
        ),
      ),
    );

    await register(TOKEN, "alice", PASSWORD);

    expect(screen.getByLabelText("Invitation token")).toHaveValue(TOKEN);
    expect(screen.getByLabelText("Username")).toHaveValue("alice");
  });

  it("states the bounds the action holds, so they are read before a submission", () => {
    render(<PasswordRegisterForm />);

    expect(
      screen.getByText(
        `Lowercase letters, digits and hyphens, at most ${USERNAME_MAX_LENGTH} characters. It names the account and does not change.`,
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByText(`At least ${PASSWORD_MIN_LENGTH} characters.`),
    ).toBeInTheDocument();
  });

  it("names every field with a label and never with a placeholder", () => {
    render(<PasswordRegisterForm />);

    for (const name of ["Invitation token", "Username", "Password"]) {
      expect(screen.getByLabelText(name)).not.toHaveAttribute("placeholder");
    }
    expect(screen.getByLabelText("Password")).toHaveAttribute(
      "type",
      "password",
    );
  });
});
