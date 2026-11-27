import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { BotHostingControl } from "@/app/console/orgs/[org]/apps/[app]/_components/bot-hosting-control";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const URL_ = `${API_URL}/orgs/acme/installations/deploy-helper/bot-hosting`;

function renderControl(granted: boolean) {
  return render(
    <BotHostingControl
      org="acme"
      installation="deploy-helper"
      name="Deploy helper"
      granted={granted}
    />,
  );
}

describe("bot hosting control", () => {
  it("grants only after the button naming the act is pressed", async () => {
    const puts: string[] = [];
    server.use(
      http.put(URL_, () => {
        puts.push("PUT");
        return HttpResponse.json({
          grn: "grn:acme/iam/installation/deploy-helper",
          orgGrn: "grn:root/iam/org/acme",
          appGrn: "grn:acme/iam/app/deploy-helper",
          incarnation: "1",
          botHosting: true,
          projects: [],
          createdBy: "grn:root/iam/user/alice",
          createdAt: "2026-11-02T08:00:00Z",
        });
      }),
    );
    const user = userEvent.setup();
    renderControl(false);

    await user.click(
      await screen.findByRole("button", { name: "Grant bot hosting" }),
    );
    expect(puts).toEqual([]);
    await user.click(
      within(screen.getByRole("alertdialog")).getByRole("button", {
        name: "Grant bot hosting",
      }),
    );

    await vi.waitFor(() => expect(puts).toEqual(["PUT"]));
  });

  it("says first that live bot tokens are revoked and declarations stay, then withdraws", async () => {
    const deletes: string[] = [];
    server.use(
      http.delete(URL_, () => {
        deletes.push("DELETE");
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const user = userEvent.setup();
    renderControl(true);

    await user.click(
      await screen.findByRole("button", { name: "Withdraw bot hosting" }),
    );
    const dialog = screen.getByRole("alertdialog");
    expect(dialog).toHaveTextContent("live bot tokens are revoked");
    expect(dialog).toHaveTextContent("declarations already placed");
    expect(dialog).toHaveTextContent("stay");
    expect(deletes).toEqual([]);

    await user.click(
      within(dialog).getByRole("button", { name: "Withdraw bot hosting" }),
    );
    await vi.waitFor(() => expect(deletes).toEqual(["DELETE"]));
  });
});
