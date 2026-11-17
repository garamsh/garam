import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import AuditPage from "@/app/console/orgs/[org]/audit/page";
import { TooltipProvider } from "@garam/design/ui/tooltip";
import { renderServerComponent } from "@/testing/render";
import {
  API_URL,
  collapsedDenial,
  guardrailCreated,
} from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  notFound: () => {
    throw new Error("NEXT_NOT_FOUND");
  },
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

const RECORDS = `${API_URL}/orgs/acme/audit-records`;

// The console layout mounts the provider a `<Time>` needs.
function renderAudit(page?: string) {
  return renderServerComponent(
    AuditPage({
      params: Promise.resolve({ org: "acme" }),
      searchParams: Promise.resolve({ page }),
    }),
    { wrapper: TooltipProvider },
  );
}

function refuse() {
  return HttpResponse.json({ kind: "denied", message: "no" }, { status: 403 });
}

describe("audit page", () => {
  it("lists each write as the service records it and a denial with its count", async () => {
    await renderAudit();

    const created = screen.getByRole("row", { name: /iam:policy\.create/ });
    expect(within(created).getByText("created")).toBeInTheDocument();
    expect(within(created).getByText("v1")).toBeInTheDocument();
    expect(within(created).getByText("Policy write")).toBeInTheDocument();
    const added = screen.getByRole("row", { name: /version-added/ });
    expect(within(added).getByText("v2")).toBeInTheDocument();
    const back = screen.getByRole("row", { name: /rolled-back/ });
    expect(within(back).getByText("v2 to v1")).toBeInTheDocument();
    const removed = screen.getByRole("row", { name: /iam:policy\.delete/ });
    expect(within(removed).getByText("removed")).toBeInTheDocument();
    expect(within(removed).queryByText(/v\d/)).not.toBeInTheDocument();
    const denial = screen.getByRole("row", { name: /iam:project\.delete/ });
    expect(within(denial).getByText("deny")).toBeInTheDocument();
    expect(within(denial).getByText("3 times")).toBeInTheDocument();
    expect(screen.queryByText("Older records")).not.toBeInTheDocument();
  });

  it("reads an overflow record as denials not stored, asked by no one", async () => {
    await renderAudit();

    const overflow = screen.getByRole("row", {
      name: /Denials over the day's limit/,
    });
    expect(
      within(overflow).getByText("40 not stored on 2026-05-01"),
    ).toBeInTheDocument();
    expect(within(overflow).getByText("Not recorded")).toBeInTheDocument();
    expect(
      within(overflow).queryByText("The platform"),
    ).not.toBeInTheDocument();
  });

  it("offers the older records through the page token", async () => {
    server.use(
      http.get(RECORDS, ({ request }) => {
        const token = new URL(request.url).searchParams.get("pageToken");
        return HttpResponse.json(
          token === "next-1"
            ? { records: [collapsedDenial] }
            : { records: [guardrailCreated], nextPageToken: "next-1" },
        );
      }),
    );

    const first = await renderAudit();
    expect(screen.getByRole("link", { name: "Older records" })).toHaveAttribute(
      "href",
      "/console/orgs/acme/audit?page=next-1",
    );
    first.unmount();

    await renderAudit("next-1");
    expect(screen.getByText("3 times")).toBeInTheDocument();
    expect(screen.queryByText("Older records")).not.toBeInTheDocument();
  });

  it("says an empty audit is empty", async () => {
    server.use(http.get(RECORDS, () => HttpResponse.json({ records: [] })));

    await renderAudit();

    expect(screen.getByText("No audit records")).toBeInTheDocument();
  });

  it("says a refused read is refused, not that there are no records", async () => {
    // The control: answered, the same page lists the records.
    const allowed = await renderAudit();
    expect(screen.getByText("3 times")).toBeInTheDocument();
    allowed.unmount();

    server.use(http.get(RECORDS, refuse));

    await renderAudit();

    expect(
      screen.getByText("You can't view this organization's audit records"),
    ).toBeInTheDocument();
    expect(screen.queryByText("No audit records")).not.toBeInTheDocument();
  });

  it("renders not-found when the organization is not the caller's", async () => {
    // The control: the organization read answered, the page renders.
    const allowed = await renderAudit();
    expect(
      screen.getByRole("heading", { level: 1, name: "Audit" }),
    ).toBeInTheDocument();
    allowed.unmount();

    server.use(
      http.get(`${API_URL}/orgs/acme`, refuse),
      http.get(RECORDS, refuse),
    );

    await expect(renderAudit()).rejects.toThrow("NEXT_NOT_FOUND");
  });

  it("says the API is unreachable, also when the policy service times out", async () => {
    // The control: answered, the same page lists the records.
    const allowed = await renderAudit();
    expect(screen.getByText("3 times")).toBeInTheDocument();
    allowed.unmount();

    server.use(http.get(RECORDS, () => HttpResponse.error()));
    const down = await renderAudit();
    expect(
      screen.getByRole("heading", { name: "Cannot reach the API" }),
    ).toBeInTheDocument();
    down.unmount();

    server.use(
      http.get(RECORDS, () =>
        HttpResponse.json(
          { kind: "deadline_exceeded", message: "no answer in time" },
          { status: 504 },
        ),
      ),
    );
    await renderAudit();
    expect(
      screen.getByRole("heading", { name: "Cannot reach the API" }),
    ).toBeInTheDocument();
    expect(screen.queryByText("No audit records")).not.toBeInTheDocument();
  });
});
