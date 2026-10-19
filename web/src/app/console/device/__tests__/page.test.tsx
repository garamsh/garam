import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import DevicePage from "@/app/console/device/page";
import DeviceRedirect from "@/app/device/page";
import { renderServerComponent } from "@/testing/render";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

const APPROVAL = `${API_URL}/auth/device/approval`;

function renderDevice(query: Record<string, string> = {}) {
  return renderServerComponent(
    DevicePage({ searchParams: Promise.resolve(query) }),
  );
}

/** Records each decision garam is sent, and answers it as decided. */
function recordDecisions() {
  const sent: unknown[] = [];
  server.use(
    http.post(APPROVAL, async ({ request }) => {
      sent.push(await request.json());
      return new HttpResponse(null, { status: 204 });
    }),
  );
  return sent;
}

describe("device page", () => {
  it("prefills the code from the address and says whom approving signs the CLI in as", async () => {
    await renderDevice({ code: "WDJB-MJHT" });

    expect(
      screen.getByRole("heading", { level: 1, name: "Sign in the CLI" }),
    ).toBeInTheDocument();
    expect(screen.getByText("This lets the CLI act as Alice.")).toBeVisible();
    expect(screen.getByRole("textbox", { name: "Code" })).toHaveValue(
      "WDJB-MJHT",
    );
  });

  it("reads RFC 8628's user_code where the address has no code", async () => {
    await renderDevice({ user_code: "ABCD-EFGH" });

    expect(screen.getByRole("textbox", { name: "Code" })).toHaveValue(
      "ABCD-EFGH",
    );
  });

  it("starts empty without a code in the address", async () => {
    // The control for the prefills above.
    await renderDevice();

    expect(screen.getByRole("textbox", { name: "Code" })).toHaveValue("");
  });

  it("approves the code it shows, and then says to return to the terminal", async () => {
    const sent = recordDecisions();
    const user = userEvent.setup();
    await renderDevice({ code: "WDJB-MJHT" });

    await user.click(screen.getByRole("button", { name: "Approve" }));

    expect(await screen.findByText("The CLI is signed in")).toBeInTheDocument();
    expect(screen.getByText("Return to your terminal.")).toBeVisible();
    expect(sent).toEqual([{ userCode: "WDJB-MJHT", approve: true }]);
    expect(screen.queryByRole("button", { name: "Approve" })).toBeNull();
  });

  it("denies the code, and says the CLI was not signed in", async () => {
    const sent = recordDecisions();
    const user = userEvent.setup();
    await renderDevice();

    await user.type(screen.getByRole("textbox", { name: "Code" }), "ABCD-EFGH");
    await user.click(screen.getByRole("button", { name: "Deny" }));

    expect(await screen.findByText("Sign-in denied")).toBeInTheDocument();
    expect(sent).toEqual([{ userCode: "ABCD-EFGH", approve: false }]);
  });

  it("says a code garam holds nothing pending for, and keeps what was typed", async () => {
    server.use(
      http.post(APPROVAL, () =>
        HttpResponse.json(
          {
            kind: "not_found",
            message: "no pending device authorization has that code",
          },
          { status: 404 },
        ),
      ),
    );
    const user = userEvent.setup();
    await renderDevice({ code: "OLDC-ODE1" });

    await user.click(screen.getByRole("button", { name: "Approve" }));

    const code = screen.getByRole("textbox", { name: "Code" });
    await vi.waitFor(() =>
      expect(code).toHaveAccessibleDescription(
        /^That code isn't waiting for approval\./,
      ),
    );
    expect(code).toHaveValue("OLDC-ODE1");
    expect(screen.queryByText("The CLI is signed in")).toBeNull();
  });
});

describe("the /device address", () => {
  it("forwards to the console's page, keeping the code", async () => {
    await expect(
      DeviceRedirect({ searchParams: Promise.resolve({ code: "WDJB MJHT" }) }),
    ).rejects.toThrow("NEXT_REDIRECT:/console/device?code=WDJB%20MJHT");
    await expect(
      DeviceRedirect({
        searchParams: Promise.resolve({ user_code: "ABCD-EFGH" }),
      }),
    ).rejects.toThrow("NEXT_REDIRECT:/console/device?code=ABCD-EFGH");
    // The control: with no code there is nothing to carry.
    await expect(
      DeviceRedirect({ searchParams: Promise.resolve({}) }),
    ).rejects.toThrow("NEXT_REDIRECT:/console/device");
  });
});
