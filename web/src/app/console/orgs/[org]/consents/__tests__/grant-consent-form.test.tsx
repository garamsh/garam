import { render, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { GrantConsentForm } from "@/app/console/orgs/[org]/consents/_components/grant-consent-form";
import { enter } from "@/testing/enter";
import { API_URL, globexConsent } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

// `revalidatePath` needs a request scope the test does not run in; the
// action's own answer is what the form renders.
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

/** Records the pairs the form granted, and answers as garam does. */
function recordGrants(stands: boolean) {
  const granted: string[] = [];
  server.use(
    http.put(`${API_URL}/orgs/acme/consents/:other`, ({ request }) => {
      granted.push(new URL(request.url).pathname);
      return HttpResponse.json({
        ...globexConsent,
        grantedThere: stands,
        stands,
      });
    }),
  );
  return granted;
}

describe("grant consent form", () => {
  it("grants the half under the pair's own path and reports the pair standing", async () => {
    const granted = recordGrants(true);
    const user = userEvent.setup();
    const { container } = render(<GrantConsentForm org="acme" />);
    const form = within(container);

    await enter(user, form.getByLabelText("Other organization"), "globex");
    await user.click(form.getByRole("button", { name: "Grant consent" }));

    expect(
      await form.findByText(
        "Granted. Messages now cross between this organization and globex.",
      ),
    ).toBeInTheDocument();
    expect(granted).toEqual(["/orgs/acme/consents/globex"]);
    // ADR-0011: the console addresses an organization by the GRN's last
    // segment and prints no GRN.
    expect(container.textContent).not.toContain("grn:");
  });

  it("reports the half waiting when the other organization has not granted", async () => {
    recordGrants(false);
    const user = userEvent.setup();
    const form = within(render(<GrantConsentForm org="acme" />).container);

    await enter(user, form.getByLabelText("Other organization"), "globex");
    await user.click(form.getByRole("button", { name: "Grant consent" }));

    expect(
      await form.findByText(
        "Granted this organization's half for globex. Messages stay refused until it grants its own.",
      ),
    ).toBeInTheDocument();
  });

  it("renders the refusal an organization consenting with itself gets", async () => {
    // The API decides what may be granted and names the refusal, which
    // is the sentence the form shows.
    server.use(
      http.put(`${API_URL}/orgs/acme/consents/acme`, () =>
        HttpResponse.json(
          {
            kind: "invalid_argument",
            message: "an organization consents with another, not with itself",
          },
          { status: 400 },
        ),
      ),
    );
    const user = userEvent.setup();
    const form = within(render(<GrantConsentForm org="acme" />).container);

    await enter(user, form.getByLabelText("Other organization"), "acme");
    await user.click(form.getByRole("button", { name: "Grant consent" }));

    expect(
      await form.findByText(
        "an organization consents with another, not with itself",
      ),
    ).toBeInTheDocument();
  });
});
