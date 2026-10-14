import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { PasswordRegisterForm } from "@/features/auth/components/password-register-form";
import {
  PASSWORD_MIN_LENGTH,
  USERNAME_MAX_LENGTH,
} from "@/lib/password-credentials";
import { enter } from "@/testing/enter";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";
import { REFUSAL_COPY } from "@/lib/refusal-copy";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

/** The password these tests register with; it is a fixture, not a value. */
const PASSWORD = "correct-horse-battery-staple";

const TOKEN = "invitation-token-nobody-guesses";

/**
 * Registers through the form and answers with what it then shows, and
 * with queries scoped to this render rather than to the document.
 */
async function register(token: string, username: string, password: string) {
  const user = userEvent.setup();
  const form = within(render(<PasswordRegisterForm />).container);

  await enter(user, form.getByLabelText("Invitation token"), token);
  await enter(user, form.getByLabelText("Username"), username);
  await enter(user, form.getByLabelText("Password"), password);
  await user.click(form.getByRole("button", { name: "Create account" }));

  return { refusal: (await form.findByRole("alert")).textContent, form };
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

    const { refusal } = await register(TOKEN, "alice", PASSWORD);

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
    expect(refusal).toBe(REFUSAL_COPY.invitationNotFound);
    expect(document.body.innerHTML).not.toContain(PASSWORD);
  });

  it("puts each refusal under the field it is about", async () => {
    server.use(
      http.post(`${API_URL}/auth/password/register`, () =>
        HttpResponse.json(
          { kind: "already_exists", message: "user: already exists" },
          { status: 409 },
        ),
      ),
    );

    const taken = await register(TOKEN, "alice", PASSWORD);

    expect(taken.form.getByLabelText("Username")).toHaveAttribute(
      "aria-invalid",
      "true",
    );
    expect(taken.form.getByLabelText("Username")).toHaveAccessibleDescription(
      /That username is taken\. Choose another one\./,
    );
    // The control: the other fields are not marked, so the mark is the
    // refusal's field and not every field the form holds.
    expect(taken.form.getByLabelText("Invitation token")).not.toHaveAttribute(
      "aria-invalid",
    );
    expect(taken.form.getByLabelText("Password")).not.toHaveAttribute(
      "aria-invalid",
    );
  });

  it("puts a spent invitation on the token field", async () => {
    server.use(
      http.post(`${API_URL}/auth/password/register`, () =>
        HttpResponse.json(
          { kind: "failed_precondition", message: "invitation: used" },
          { status: 409 },
        ),
      ),
    );

    const spent = await register(TOKEN, "alice", PASSWORD);

    const token = spent.form.getByLabelText("Invitation token");
    expect(token).toHaveAttribute("aria-invalid", "true");
    expect(token).toHaveAccessibleDescription(
      new RegExp(REFUSAL_COPY.invitationSpent.replace(/[.?]/g, "\\$&")),
    );
    // The control: the username is not marked for the invitation.
    expect(spent.form.getByLabelText("Username")).not.toHaveAttribute(
      "aria-invalid",
    );
  });

  it("says an invitation refusal above the button when the token field is hidden", async () => {
    server.use(
      http.post(`${API_URL}/auth/password/register`, () =>
        HttpResponse.json(
          { kind: "not_found", message: "invitation not found" },
          { status: 404 },
        ),
      ),
    );
    const user = userEvent.setup();
    const form = within(
      render(<PasswordRegisterForm invitationToken={TOKEN} />).container,
    );
    expect(form.queryByLabelText("Invitation token")).toBeNull();
    await enter(user, form.getByLabelText("Username"), "alice");
    await enter(user, form.getByLabelText("Password"), PASSWORD);
    await user.click(form.getByRole("button", { name: "Create account" }));

    expect(await form.findByRole("alert")).toHaveTextContent(
      REFUSAL_COPY.invitationNotFound,
    );
    // The control: the shown fields are not blamed for the invitation.
    for (const label of ["Username", "Password"]) {
      expect(form.getByLabelText(label)).not.toHaveAttribute("aria-invalid");
    }
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

    const { form } = await register(TOKEN, "alice", PASSWORD);

    expect(form.getByLabelText("Invitation token")).toHaveValue(TOKEN);
    expect(form.getByLabelText("Username")).toHaveValue("alice");
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
