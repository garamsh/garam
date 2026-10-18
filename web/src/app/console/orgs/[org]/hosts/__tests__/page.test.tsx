import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import HostsPage from "@/app/console/orgs/[org]/hosts/page";
import { TooltipProvider } from "@/components/ui/tooltip";
import { REFUSAL_COPY } from "@/lib/refusal-copy";
import { renderServerComponent } from "@/testing/render";
import {
  API_URL,
  answerPermissionChecks,
  buildHost,
  pendingApproval,
} from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  notFound: () => {
    throw new Error("NEXT_NOT_FOUND");
  },
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
  usePathname: () => "/console/orgs/acme/hosts",
  useRouter: () => ({ refresh: () => {} }),
}));

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

// Radix Select measures with ResizeObserver, which jsdom lacks.
vi.stubGlobal(
  "ResizeObserver",
  class {
    observe() {}
    unobserve() {}
    disconnect() {}
  },
);

const PREPARED = JSON.stringify({
  requestId: "host-7f3a",
  secretSha256: "a".repeat(64),
});

function renderHosts() {
  return renderServerComponent(
    HostsPage({ params: Promise.resolve({ org: "acme" }) }),
    { wrapper: TooltipProvider },
  );
}

/** Opens the approval dialog and fills it as a person would. */
async function approve(
  user: ReturnType<typeof userEvent.setup>,
  prepared: string,
) {
  await user.click(screen.getByRole("button", { name: "Approve a host" }));
  const dialog = screen.getByRole("alertdialog", { name: "Approve a host" });
  await user.click(
    within(dialog).getByRole("textbox", {
      name: "What garam standalone prepare printed",
    }),
  );
  await user.paste(prepared);
  await user.type(
    within(dialog).getByRole("textbox", { name: "Host name" }),
    "build-09",
  );
  await user.click(
    within(dialog).getByRole("button", { name: "Approve host" }),
  );
  return dialog;
}

describe("hosts", () => {
  it("lists supervisors with their agent and epoch, and approvals with their state", async () => {
    await renderHosts();

    const supervisors = screen.getByRole("table", { name: "Supervisors" });
    const row = within(supervisors)
      // The host's name and its supervisor's identifier match here.
      .getAllByText(buildHost.hostName)[0]
      .closest("tr");
    expect(row).toHaveTextContent("Active");
    expect(row).toHaveTextContent("Scout, epoch 2");
    const approvals = screen.getByRole("table", { name: "Approvals" });
    expect(
      within(approvals).getByText(pendingApproval.hostName).closest("tr"),
    ).toHaveTextContent("Waiting for the host");
  });

  it("approves a host from what prepare printed, sending its fields alone", async () => {
    const sent: unknown[] = [];
    server.use(
      http.post(
        `${API_URL}/orgs/acme/agent-enrollments`,
        async ({ request }) => {
          sent.push(await request.json());
          return HttpResponse.json({}, { status: 201 });
        },
      ),
    );
    const user = userEvent.setup();
    await renderHosts();

    await approve(user, PREPARED);

    expect(await screen.findByText("Approved build-09")).toBeInTheDocument();
    expect(sent).toEqual([
      expect.objectContaining({
        requestId: "host-7f3a",
        secretSha256: "a".repeat(64),
        hostName: "build-09",
        placement: "standalone",
      }),
    ]);
    // Within the server's limit of a day, and an hour by default.
    const { expiresAt } = sent[0] as { expiresAt: string };
    const lasts = Date.parse(expiresAt) - Date.now();
    expect(lasts).toBeGreaterThan(55 * 60_000);
    expect(lasts).toBeLessThanOrEqual(60 * 60_000);
  });

  it("refuses a paste that is not prepare's, and says nothing pasted back", async () => {
    let posted = 0;
    server.use(
      http.post(`${API_URL}/orgs/acme/agent-enrollments`, () => {
        posted += 1;
        return HttpResponse.json({}, { status: 201 });
      }),
    );
    const user = userEvent.setup();
    await renderHosts();

    const dialog = await approve(
      user,
      JSON.stringify({ requestId: "host-7f3a", secret: "s3cr3t-value" }),
    );

    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      REFUSAL_COPY.preparedUnreadable,
    );
    expect(dialog).not.toHaveTextContent("s3cr3t-value");
    expect(posted).toBe(0);
  });

  it("refuses a field prepare does not print, beside every field it does", async () => {
    let posted = 0;
    server.use(
      http.post(`${API_URL}/orgs/acme/agent-enrollments`, () => {
        posted += 1;
        return HttpResponse.json({}, { status: 201 });
      }),
    );
    const user = userEvent.setup();
    await renderHosts();

    // Every field prepare prints is valid here, so the extra one alone
    // is what is refused.
    const dialog = await approve(
      user,
      JSON.stringify({ ...JSON.parse(PREPARED), bootstrap: "kept-on-host" }),
    );

    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      REFUSAL_COPY.preparedUnreadable,
    );
    expect(posted).toBe(0);
  });

  it("withdraws an approval no host has spent, after saying what ends", async () => {
    const withdrawn: string[] = [];
    server.use(
      http.delete(
        `${API_URL}/orgs/acme/agent-enrollments/:id`,
        ({ params }) => {
          withdrawn.push(String(params.id));
          return new HttpResponse(null, { status: 204 });
        },
      ),
    );
    const user = userEvent.setup();
    await renderHosts();

    await user.click(
      screen.getByRole("button", {
        name: `Withdraw the approval for ${pendingApproval.hostName}`,
      }),
    );
    await user.click(screen.getByRole("button", { name: "Withdraw approval" }));

    await vi.waitFor(() => expect(withdrawn).toEqual([pendingApproval.id]));
  });

  it("revokes an active supervisor by its identifier", async () => {
    const revoked: string[] = [];
    server.use(
      http.delete(
        `${API_URL}/orgs/acme/agent-supervisors/:supervisor`,
        ({ params }) => {
          revoked.push(String(params.supervisor));
          return new HttpResponse(null, { status: 204 });
        },
      ),
    );
    const user = userEvent.setup();
    await renderHosts();

    await user.click(
      screen.getByRole("button", { name: "Revoke build-07's supervisor" }),
    );
    expect(screen.getByRole("alertdialog")).toHaveTextContent(
      /Scout stops running on build-07/,
    );
    await user.click(screen.getByRole("button", { name: "Revoke supervisor" }));

    await vi.waitFor(() => expect(revoked).toEqual(["build-07"]));
  });

  it("keeps approving, withdrawing and revoking on the page, inactive, where the server denies agent:enroll", async () => {
    server.use(
      answerPermissionChecks((action) =>
        action === "agent:enroll" ? "deny" : "allow",
      ),
    );

    await renderHosts();

    for (const name of ["Approve a host", "Withdraw", "Revoke"]) {
      const button = screen.getByRole("button", { name });
      expect(button).toHaveAttribute("aria-disabled", "true");
      expect(button).toHaveAccessibleDescription(/needs agent:enroll/);
    }
  });

  it("says a refused list rather than none", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/agent-supervisors`, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "no" },
          { status: 403 },
        ),
      ),
    );

    await renderHosts();

    expect(
      screen.getByText("You can't view this organization's supervisors"),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("table", { name: "Supervisors" }),
    ).not.toBeInTheDocument();
  });
});
