import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { WithdrawConsentForm } from "@/app/console/orgs/[org]/consents/_components/withdraw-consent-form";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

// `revalidatePath` needs a request scope the test does not run in; the
// list it refreshes is what reports the half is down.
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

/** Records the halves the form withdrew, and answers as garam does. */
function recordWithdrawals() {
  const withdrawn: string[] = [];
  server.use(
    http.delete(`${API_URL}/orgs/acme/consents/:other`, ({ request }) => {
      withdrawn.push(new URL(request.url).pathname);
      return new HttpResponse(null, { status: 204 });
    }),
  );
  return withdrawn;
}

describe("withdraw consent form", () => {
  it("withdraws on the confirmation and not on the click that asks for it", async () => {
    // The refusal and its control are read together: the first click
    // sends nothing, and the same recorder shows the confirmed one
    // sending — so what held the request back is the confirmation
    // rather than a form that never posts.
    const withdrawn = recordWithdrawals();
    const user = userEvent.setup();
    render(<WithdrawConsentForm org="acme" other="globex" stands />);

    await user.click(screen.getByRole("button", { name: "Withdraw" }));

    expect(withdrawn).toEqual([]);

    await user.click(
      screen.getByRole("button", { name: "Confirm withdrawal" }),
    );

    expect(withdrawn).toEqual(["/orgs/acme/consents/globex"]);
  });

  it("sends nothing when the confirmation is cancelled", async () => {
    const withdrawn = recordWithdrawals();
    const user = userEvent.setup();
    render(<WithdrawConsentForm org="acme" other="globex" stands />);

    await user.click(screen.getByRole("button", { name: "Withdraw" }));
    await user.click(screen.getByRole("button", { name: "Cancel" }));

    expect(withdrawn).toEqual([]);
    expect(
      screen.getByRole("button", { name: "Withdraw" }),
    ).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Withdraw" }));
    await user.click(
      screen.getByRole("button", { name: "Confirm withdrawal" }),
    );

    expect(withdrawn).toEqual(["/orgs/acme/consents/globex"]);
  });

  it("says messages stop when the pair stands", async () => {
    const user = userEvent.setup();
    render(<WithdrawConsentForm org="acme" other="globex" stands />);

    await user.click(screen.getByRole("button", { name: "Withdraw" }));

    expect(
      screen.getByText(
        "Messages between this organization and globex stop at once. globex keeps its own half, so granting again here would let them cross.",
      ),
    ).toBeInTheDocument();
  });

  it("says nothing crosses when the other half is down", async () => {
    const user = userEvent.setup();
    render(<WithdrawConsentForm org="acme" other="globex" stands={false} />);

    await user.click(screen.getByRole("button", { name: "Withdraw" }));

    expect(
      screen.getByText(
        "Nothing crosses today, because globex has not granted its half, and withdrawing removes the relation.",
      ),
    ).toBeInTheDocument();
  });

  it("renders the refusal the API answers with", async () => {
    server.use(
      http.delete(`${API_URL}/orgs/acme/consents/globex`, () =>
        HttpResponse.json(
          { kind: "denied", message: "the admin role is required" },
          { status: 403 },
        ),
      ),
    );
    const user = userEvent.setup();
    render(<WithdrawConsentForm org="acme" other="globex" stands />);

    await user.click(screen.getByRole("button", { name: "Withdraw" }));
    await user.click(
      screen.getByRole("button", { name: "Confirm withdrawal" }),
    );

    expect(
      await screen.findByText("the admin role is required"),
    ).toBeInTheDocument();
  });
});
