import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { renderToString } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

import { MintEnrollmentForm } from "@/app/console/orgs/[org]/operators/[operator]/_components/mint-enrollment-form";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

// `revalidatePath` needs a request scope the test does not run in; a
// re-read page is rendered here by re-rendering with its new props.
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const ENROLLMENT = `${API_URL}/orgs/acme/operators/:operator/enrollment`;
const MINTED = {
  token: "c9f0f895fb98ab9159f51fd0297e236d",
  expiresAt: "2026-09-28T10:10:00Z",
};
const REFUSAL =
  "This operator is already enrolled. A new token cannot be issued.";

/** Records the operators a token was minted for, and answers `answer`. */
function recordMints(answer: () => Response) {
  const minted: string[] = [];
  server.use(
    http.post(ENROLLMENT, ({ params }) => {
      minted.push(String(params.operator));
      return answer();
    }),
  );
  return minted;
}

function form(enrolled: boolean) {
  return (
    <MintEnrollmentForm org="acme" operator="field-two" enrolled={enrolled} />
  );
}

describe("mint enrollment form", () => {
  it("submits nothing before hydration: the server render disables Issue replacement token", () => {
    const html = document.createElement("div");
    html.innerHTML = renderToString(form(false));
    expect(
      html.querySelector('button[type="submit"]')?.hasAttribute("disabled"),
    ).toBe(true);

    // The control: rendered in the browser, the same button is enabled.
    render(form(false));
    expect(
      screen.getByRole("button", { name: "Issue replacement token" }),
    ).toBeEnabled();
  });

  it("mints for this operator and shows the token with its expiry, that it replaces any previous token, and where it goes", async () => {
    const minted = recordMints(() =>
      HttpResponse.json(MINTED, { status: 201 }),
    );
    const user = userEvent.setup();
    render(form(false));

    await user.click(
      screen.getByRole("button", { name: "Issue replacement token" }),
    );

    expect(await screen.findByLabelText("Enrollment token")).toHaveValue(
      MINTED.token,
    );
    expect(minted).toEqual(["field-two"]);
    expect(
      screen.getByText("Save this token now. It is shown only once."),
    ).toBeInTheDocument();
    // A regex: the expiry renders in the reader's own time zone, so the
    // time inside this sentence is the one part that is not fixed.
    expect(
      screen.getByText(/^Expires at .*\. Replaces any previous token\.$/),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Update the operator’s enrollment token Secret."),
    ).toBeInTheDocument();
    expect(screen.getByRole("time").getAttribute("dateTime")).toBe(
      MINTED.expiresAt,
    );
  });

  it("shows the token only to the answer that minted it: a fresh render of the page has none", async () => {
    recordMints(() => HttpResponse.json(MINTED, { status: 201 }));
    const user = userEvent.setup();
    const first = render(form(false));

    await user.click(
      screen.getByRole("button", { name: "Issue replacement token" }),
    );
    expect(await screen.findByLabelText("Enrollment token")).toHaveValue(
      MINTED.token,
    );

    // The page re-read after the mint reads the operator as pending and
    // keeps the panel; leaving and coming back mounts a new form.
    first.rerender(form(false));
    expect(screen.getByLabelText("Enrollment token")).toHaveValue(MINTED.token);
    first.unmount();

    render(form(false));
    expect(screen.queryByLabelText("Enrollment token")).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Issue replacement token" }),
    ).toBeInTheDocument();
  });

  it("keeps the already-enrolled refusal after the page re-reads the operator as enrolled", async () => {
    recordMints(() =>
      HttpResponse.json(
        { kind: "failed_precondition", message: "garam's own sentence" },
        { status: 409 },
      ),
    );
    const user = userEvent.setup();
    const { rerender, unmount } = render(form(false));

    await user.click(
      screen.getByRole("button", { name: "Issue replacement token" }),
    );
    expect(await screen.findByText(REFUSAL)).toHaveAttribute("role", "alert");

    // What the action's revalidation renders: the same form, now told
    // the operator enrolled.
    rerender(form(true));
    expect(screen.getByText(REFUSAL)).toHaveAttribute("role", "alert");
    expect(
      screen.queryByRole("button", { name: "Issue replacement token" }),
    ).not.toBeInTheDocument();
    unmount();

    // The control: an enrolled operator rendered without that answer
    // shows no refusal, so the one above is the answer's, kept.
    render(form(true));
    expect(screen.queryByText(REFUSAL)).not.toBeInTheDocument();
  });

  it("offers no mint for an operator that enrolled", () => {
    render(form(true));
    expect(
      screen.queryByRole("button", { name: "Issue replacement token" }),
    ).not.toBeInTheDocument();

    // The control: the same form for an operator without a certificate
    // offers it, so the absence above is the certificate's.
    render(form(false));
    expect(
      screen.getByRole("button", { name: "Issue replacement token" }),
    ).toBeInTheDocument();
  });

  it("shows any other refusal as garam said it and keeps the control to try again", async () => {
    let refusing = true;
    recordMints(() =>
      refusing
        ? HttpResponse.json(
            { kind: "permission_denied", message: "permission denied" },
            { status: 403 },
          )
        : HttpResponse.json(MINTED, { status: 201 }),
    );
    const user = userEvent.setup();
    render(form(false));

    await user.click(
      screen.getByRole("button", { name: "Issue replacement token" }),
    );

    expect(await screen.findByText("permission denied")).toHaveAttribute(
      "role",
      "alert",
    );
    expect(
      screen.getByRole("button", { name: "Issue replacement token" }),
    ).toBeEnabled();
    expect(screen.queryByLabelText("Enrollment token")).not.toBeInTheDocument();

    // The control: the same button, once garam accepts, shows a token.
    refusing = false;
    await user.click(
      screen.getByRole("button", { name: "Issue replacement token" }),
    );
    expect(await screen.findByLabelText("Enrollment token")).toHaveValue(
      MINTED.token,
    );
  });
});
