import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { PasswordSignInForm } from "@/features/auth/components/password-sign-in-form";
import { PASSWORD_REFUSED } from "@/lib/password-credentials";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

/** The password these tests sign in with; it is a fixture, not a value. */
const PASSWORD = "correct-horse-battery-staple";

/** Answers as garam does when a sign-in is refused. */
function refuses(status: number, kind: string, message: string) {
  server.use(
    http.post(`${API_URL}/auth/password/login`, () =>
      HttpResponse.json({ kind, message }, { status }),
    ),
  );
}

/**
 * Signs in through the form and answers with what the page then shows:
 * the refusal on its own, and the whole rendered text around it.
 */
async function signIn(username: string, password: string) {
  const user = userEvent.setup();
  const { container } = render(<PasswordSignInForm />);

  await user.type(screen.getByLabelText("Username"), username);
  await user.type(screen.getByLabelText("Password"), password);
  await user.click(screen.getByRole("button", { name: "Sign in" }));

  const refusal = await screen.findByRole("alert");
  return { refusal: refusal.textContent, rendered: container.textContent };
}

describe("password sign-in form", () => {
  it("renders one refusal for a wrong password and for a username no account holds", async () => {
    // garam refuses the two identically (ADR-0041). These two answers
    // differ on purpose: the sentence the form renders is the
    // console's own, so what a person reads cannot come to name which
    // of the two it was.
    refuses(401, "unauthenticated", "no account holds that username");
    const unknownUsername = await signIn("mallory", PASSWORD);
    cleanup();

    refuses(401, "unauthenticated", "that password is wrong");
    const wrongPassword = await signIn("alice", "not-the-password-at-all");
    cleanup();

    expect(unknownUsername.refusal).toBe(PASSWORD_REFUSED);
    expect(unknownUsername).toEqual(wrongPassword);

    // The control: a status that refuses no credential renders garam's
    // own message, so the sentence above is one the form chooses
    // rather than every message it drops.
    refuses(
      429,
      "resource_exhausted",
      "too many password verifications are live",
    );
    const bounded = await signIn("alice", PASSWORD);

    expect(bounded.refusal).toBe("too many password verifications are live");
  });

  it("asks nothing about the username before sending it", async () => {
    // A username registration would refuse on its shape is sent all the
    // same and comes back as the one refusal. A form holding that rule
    // here would answer a third way and say which of the two it had.
    const sent: unknown[] = [];
    server.use(
      http.post(`${API_URL}/auth/password/login`, async ({ request }) => {
        sent.push(await request.json());
        return HttpResponse.json(
          { kind: "unauthenticated", message: "no" },
          { status: 401 },
        );
      }),
    );

    const refused = await signIn("Mallory", PASSWORD);

    expect(refused.refusal).toBe(PASSWORD_REFUSED);
    expect(sent).toEqual([{ username: "Mallory", password: PASSWORD }]);
  });

  it("sends the password in the body of a POST and puts it on no page", async () => {
    const seen: { method: string; url: string }[] = [];
    server.use(
      http.post(`${API_URL}/auth/password/login`, ({ request }) => {
        seen.push({ method: request.method, url: request.url });
        return HttpResponse.json(
          { kind: "unauthenticated", message: "no" },
          { status: 401 },
        );
      }),
    );

    const refused = await signIn("alice", PASSWORD);

    expect(seen).toEqual([
      { method: "POST", url: `${API_URL}/auth/password/login` },
    ]);
    expect(refused.rendered).not.toContain(PASSWORD);
    expect(document.body.innerHTML).not.toContain(PASSWORD);
  });

  it("keeps the typed username when the API does not answer", async () => {
    server.use(
      http.post(`${API_URL}/auth/password/login`, () => HttpResponse.error()),
    );

    const refused = await signIn("alice", PASSWORD);

    expect(refused.refusal).toBe("the garam API could not be reached");
    expect(screen.getByLabelText("Username")).toHaveValue("alice");
  });

  it("names both fields with a label and never with a placeholder", () => {
    render(<PasswordSignInForm />);

    // A placeholder is gone as soon as the field is typed into, so a
    // form that named its fields that way names them only while empty.
    expect(screen.getByLabelText("Username")).not.toHaveAttribute(
      "placeholder",
    );
    expect(screen.getByLabelText("Password")).not.toHaveAttribute(
      "placeholder",
    );
    // The password is not on screen in the clear.
    expect(screen.getByLabelText("Password")).toHaveAttribute(
      "type",
      "password",
    );
  });
});
