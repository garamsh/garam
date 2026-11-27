import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import InstancePage from "@/app/console/orgs/[org]/apps/[app]/[instance]/page";
import { TooltipProvider } from "@garam/design/ui/tooltip";
import {
  API_URL,
  answerPermissionChecks,
  fieldBot,
  fieldBotKey,
} from "@/testing/mocks/handlers";
import { renderServerComponent } from "@/testing/render";
import { server } from "@/testing/setup";

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));
vi.mock("next/navigation", () => ({
  notFound: () => {
    throw new Error("NEXT_NOT_FOUND");
  },
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
  usePathname: () => "/console/orgs/acme/apps/garam-bot-connector/i-1a2b3c",
  useRouter: () => ({ refresh: () => {} }),
}));

const INSTANCE = `${API_URL}/orgs/acme/installations/garam-bot-connector/instances/i-1a2b3c`;

function renderPage(tab?: string) {
  return renderServerComponent(
    InstancePage({
      params: Promise.resolve({
        org: "acme",
        app: "garam-bot-connector",
        instance: "i-1a2b3c",
      }),
      searchParams: Promise.resolve(tab ? { tab } : {}),
    }),
    { wrapper: TooltipProvider },
  );
}

function fact(term: string) {
  return screen.getByText(term, { selector: "dt" })
    .nextElementSibling as HTMLElement;
}

describe("instance page", () => {
  it("titles the page with its name and offers its identifier to copy, with no GRN", async () => {
    const { container } = await renderPage();

    expect(
      screen.getByRole("heading", { level: 1, name: "Field bot" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("group", { name: "Identifier" })).toHaveTextContent(
      "i-1a2b3c",
    );
    expect(container.textContent).not.toContain("grn:");
  });

  it("shows the kind as the text it was stored as, and the last contact", async () => {
    server.use(
      http.get(INSTANCE, () =>
        HttpResponse.json({ ...fieldBot, kind: "Anything <b>goes</b> 🚀" }),
      ),
    );

    await renderPage();

    expect(fact("Kind")).toHaveTextContent("Anything <b>goes</b> 🚀");
    expect(fact("Last contact")).not.toHaveTextContent("Never");
    expect(fact("Created")).toHaveTextContent("by Alice");
  });

  it("says never where the instance has made no contact", async () => {
    server.use(
      http.get(`${INSTANCE}/report`, () =>
        HttpResponse.json({ lastContactAt: null }),
      ),
    );

    await renderPage();

    expect(fact("Last contact")).toHaveTextContent("Never");
  });

  it("lists its keys with their state and offers revoking and reissuing", async () => {
    await renderPage("keys");

    const row = within(
      screen.getByRole("table", { name: "Keys" }),
    ).getAllByRole("row")[1];
    expect(row).toHaveTextContent(fieldBotKey.kid.slice(0, 12));
    expect(row).toHaveTextContent("Active");
    expect(
      within(row).getByRole("button", { name: "Revoke" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Reissue registration token" }),
    ).toBeInTheDocument();
  });

  it("says a retiring key's end and a revoked key's, and offers no revoke on the latter", async () => {
    server.use(
      http.get(`${INSTANCE}/keys`, () =>
        HttpResponse.json({
          keys: [
            {
              ...fieldBotKey,
              state: "retiring",
              retiresAt: "2026-11-20T00:00:00Z",
            },
            {
              ...fieldBotKey,
              kid: "other-kid-0123456789",
              state: "revoked",
              revokedAt: "2026-11-05T00:00:00Z",
            },
          ],
        }),
      ),
    );

    await renderPage("keys");

    const rows = screen.getAllByRole("row");
    expect(rows[1]).toHaveTextContent("Retiring");
    expect(rows[1]).toHaveTextContent("until");
    expect(rows[2]).toHaveTextContent("Revoked");
    expect(
      within(rows[2]).queryByRole("button", { name: "Revoke" }),
    ).not.toBeInTheDocument();
  });

  it("leaves reissuing, revoking and deleting out for a caller the server denies them", async () => {
    server.use(
      answerPermissionChecks((action) =>
        action === "iam:instance.reissue" || action === "iam:instance.delete"
          ? "deny"
          : "allow",
      ),
    );

    await renderPage("keys");

    expect(
      screen.queryByRole("button", { name: "Reissue registration token" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Revoke" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Delete instance" }),
    ).not.toBeInTheDocument();
  });

  it("revokes a key after asking", async () => {
    const revoked: string[] = [];
    server.use(
      http.delete(`${INSTANCE}/keys/:kid`, ({ params }) => {
        revoked.push(String(params.kid));
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const user = userEvent.setup();
    await renderPage("keys");

    await user.click(await screen.findByRole("button", { name: "Revoke" }));
    const dialog = within(
      screen.getByRole("alertdialog", { name: "Revoke this key?" }),
    );
    expect(revoked).toEqual([]);
    await user.click(dialog.getByRole("button", { name: "Revoke key" }));

    expect(await screen.findByRole("status")).toHaveTextContent("Revoked");
    expect(revoked).toEqual([fieldBotKey.kid]);
  });

  it("reissues the registration token and shows it once", async () => {
    server.use(
      http.post(`${INSTANCE}/registration-token`, () =>
        HttpResponse.json({
          registrationToken: "fresh-token-once",
          expiresAt: "2026-11-03T09:00:00Z",
        }),
      ),
    );
    const user = userEvent.setup();
    await renderPage("keys");

    await user.click(
      await screen.findByRole("button", { name: "Reissue registration token" }),
    );
    const ask = within(screen.getByRole("alertdialog"));
    expect(
      ask.getByText(/every key Field bot holds is withdrawn/),
    ).toBeInTheDocument();
    await user.click(ask.getByRole("button", { name: "Reissue token" }));

    const reveal = await screen.findByRole("alertdialog", {
      name: "Registration token",
    });
    expect(reveal).toHaveTextContent("fresh-token-once");
    await user.click(within(reveal).getByRole("checkbox"));
    await user.click(within(reveal).getByRole("button", { name: "Done" }));
    expect(screen.queryByText("fresh-token-once")).not.toBeInTheDocument();
  });
});
