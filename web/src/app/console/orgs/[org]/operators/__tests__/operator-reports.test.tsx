import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import OperatorPage from "@/app/console/orgs/[org]/operators/[operator]/page";
import OperatorsPage from "@/app/console/orgs/[org]/operators/page";
import { TooltipProvider } from "@/components/ui/tooltip";
import {
  API_URL,
  answerPermissionChecks,
  fieldOne,
} from "@/testing/mocks/handlers";
import { renderServerComponent } from "@/testing/render";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  notFound: () => {
    throw new Error("NEXT_NOT_FOUND");
  },
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
  usePathname: () => "/console/orgs/acme/operators/field-one",
  useRouter: () => ({ refresh: () => {} }),
}));

// The authority is minted by a Server Action; what the pages do with
// the control service's answer is the subject here.
vi.mock(import("@/features/orgs/actions"), async (original) => ({
  ...(await original()),
  mintOperationAuthority: async () => ({
    status: "ok" as const,
    authority: "test-authority",
  }),
}));

const ENDPOINT = "https://control.example.test/garam";
const REPORTS = `${ENDPOINT}/v1/orgs/acme/operators`;
const DIGEST = `sha256:${"ab".repeat(32)}`;

/** The hosted operator whose control service holds the reports. */
const hosted = {
  ...fieldOne,
  grn: "grn:acme:default:operator:hosted",
  name: "Hosted control",
  controlEndpoint: ENDPOINT,
};

/**
 * Acme with a hosted operator beside `fieldOne`, whose grant carries
 * `operations`, and whose control service answers the reports read
 * with `answer`.
 */
function hostedAnswering(
  answer: () => Response,
  operations: string[] = ["operator:read"],
) {
  server.use(
    http.get(`${API_URL}/orgs/acme/operators`, () =>
      HttpResponse.json([fieldOne, hosted]),
    ),
    http.get(`${API_URL}/orgs/acme/operators/hosted/delegation`, () =>
      HttpResponse.json({
        grantId: "g-1",
        operatorGrn: hosted.grn,
        controllers: [fieldOne.grn],
        operations,
        expiresAt: "2099-01-01T00:00:00Z",
        grantedBy: "grn:root:default:user:alice",
        grantedAt: "2026-10-14T00:00:00Z",
      }),
    ),
    http.get(`${API_URL}/orgs/acme/operators/field-one/delegation`, () =>
      HttpResponse.json(
        { kind: "not_found", message: "none" },
        { status: 404 },
      ),
    ),
    http.get(REPORTS, answer),
  );
}

/** A report from `fieldOne`: two images, one with a digest. */
const reported = {
  grn: fieldOne.grn,
  images: [
    { name: "manager", ref: "ghcr.io/garamsh/operator:v0.4.0", digest: DIGEST },
    { name: "agent", ref: "ghcr.io/garamsh/gagent:v1.2.0" },
  ],
  reportedAt: "2026-10-14T08:00:00Z",
  lastContactAt: "2026-10-15T09:30:00Z",
};

function renderOperator() {
  return renderServerComponent(
    OperatorPage({
      params: Promise.resolve({ org: "acme", operator: "field-one" }),
      searchParams: Promise.resolve({}),
    }),
    { wrapper: TooltipProvider },
  );
}

function renderList() {
  return renderServerComponent(
    OperatorsPage({
      params: Promise.resolve({ org: "acme" }),
      searchParams: Promise.resolve({}),
    }),
    { wrapper: TooltipProvider },
  );
}

/** The value a fact of the overview's description list holds. */
function fact(term: string) {
  const dt = screen.getByText(term, { selector: "dt" });
  return dt.nextElementSibling as HTMLElement;
}

/** The cell of `column` in the row naming `operator`. */
function cell(operator: string, column: string) {
  const table = screen.getByRole("table", { name: "Operators" });
  const headers = within(table)
    .getAllByRole("columnheader")
    .map((header) => header.textContent);
  const row = within(table)
    .getAllByRole("row")
    .find((r) => within(r).queryByRole("link", { name: operator }));
  if (row === undefined) throw new Error(`no row for ${operator}`);
  return within(row).getAllByRole("cell")[headers.indexOf(column)]!;
}

describe("an operator's reported images and last contact", () => {
  it("shows each image by name, reference and digest, when they were reported, and the last contact, with no ISO date", async () => {
    hostedAnswering(() => HttpResponse.json({ operators: [reported] }));

    const { container } = await renderOperator();

    const images = fact("Images");
    expect(
      await within(images).findByText("ghcr.io/garamsh/operator:v0.4.0"),
    ).toBeInTheDocument();
    expect(images).toHaveTextContent("manager");
    expect(images).toHaveTextContent(DIGEST);
    expect(images).toHaveTextContent("ghcr.io/garamsh/gagent:v1.2.0");
    // The reference with no digest is shown without one, not with a
    // made-up value.
    expect(within(images).getAllByText(/^sha256:/)).toHaveLength(1);
    // Presence: the word is followed by the time it names.
    const when = within(images).getByText(/^Reported/);
    expect(within(when).getByRole("time")).toHaveAttribute(
      "datetime",
      "2026-10-14T08:00:00.000Z",
    );
    expect(within(fact("Last contact")).getByRole("time")).toHaveAttribute(
      "datetime",
      "2026-10-15T09:30:00.000Z",
    );
    expect(container.textContent).not.toMatch(/\d{4}-\d{2}-\d{2}T/);
    expect(container.textContent).not.toContain("grn:");
  });

  it("says not reported, never an empty list, for an operator the answer leaves out or that reported no image", async () => {
    // The control: the same read with a report shows it, so the words
    // below come from what the answer holds, not from the read failing.
    hostedAnswering(() => HttpResponse.json({ operators: [reported] }));
    const shown = await renderOperator();
    expect(
      await within(fact("Images")).findByText("ghcr.io/garamsh/gagent:v1.2.0"),
    ).toBeInTheDocument();
    shown.unmount();

    hostedAnswering(() => HttpResponse.json({ operators: [] }));
    const absent = await renderOperator();
    expect(
      await within(fact("Images")).findByText("Not reported"),
    ).toBeInTheDocument();
    expect(fact("Last contact")).toHaveTextContent("No contact recorded");
    absent.unmount();

    hostedAnswering(() =>
      HttpResponse.json({
        operators: [
          {
            grn: fieldOne.grn,
            images: [],
            lastContactAt: "2026-10-15T09:30:00Z",
          },
        ],
      }),
    );
    await renderOperator();
    expect(
      await within(fact("Images")).findByText("Not reported"),
    ).toBeInTheDocument();
    expect(within(fact("Last contact")).getByRole("time")).toBeInTheDocument();
    expect(within(fact("Images")).queryByRole("list")).not.toBeInTheDocument();
  });

  it("says not checked, in the console's words, where the control service refuses the read", async () => {
    hostedAnswering(() =>
      HttpResponse.json(
        { message: "operator:read is not delegated to you" },
        { status: 403 },
      ),
    );

    const { container } = await renderOperator();

    expect(
      await screen.findByText(
        "The control service refused this for you: garam no longer authorizes it, or the operator's delegation no longer covers it. Reload the page to see what you can do now.",
      ),
    ).toBeInTheDocument();
    expect(fact("Images")).toHaveTextContent("Not checked");
    expect(fact("Last contact")).toHaveTextContent("Not checked");
    expect(container.textContent).not.toContain("not delegated to you");
  });

  it("says not checked, and why, where no grant carries operator:read, and asks the control service nothing", async () => {
    let asked = 0;
    hostedAnswering(() => {
      asked += 1;
      return HttpResponse.json({ operators: [reported] });
    }, ["agent-template:read"]);

    await renderOperator();

    expect(
      screen.getByText(
        "Hosted control's delegation does not include “Read operator status”.",
      ),
    ).toBeInTheDocument();
    expect(fact("Images")).toHaveTextContent("Not checked");
    expect(fact("Last contact")).toHaveTextContent("Not checked");
    expect(asked).toBe(0);
  });

  it("leaves the facts out only where the server denies operator:read, and shows them where it cannot say", async () => {
    hostedAnswering(() => HttpResponse.json({ operators: [reported] }));
    server.use(
      answerPermissionChecks((action) =>
        action === "operator:read" ? "deny" : "allow",
      ),
    );
    const denied = await renderOperator();
    expect(
      screen.queryByText("Images", { selector: "dt" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByText("Last contact", { selector: "dt" }),
    ).not.toBeInTheDocument();
    // The control: the rest of the overview is there.
    expect(fact("Description")).toHaveTextContent("Runs the batch agents.");
    denied.unmount();

    server.use(
      answerPermissionChecks((action) =>
        action === "operator:read" ? "unknown" : "allow",
      ),
    );
    await renderOperator();
    expect(
      await within(fact("Images")).findByText("ghcr.io/garamsh/gagent:v1.2.0"),
    ).toBeInTheDocument();
  });

  it("lists each operator's images and last contact, and an operator that never reached the control service as not reported", async () => {
    hostedAnswering(() => HttpResponse.json({ operators: [reported] }));

    await renderList();

    expect(
      await within(cell("Field cluster one", "Images")).findByText(
        "ghcr.io/garamsh/gagent:v1.2.0",
      ),
    ).toBeInTheDocument();
    // The list leaves the digest to the operator's page.
    expect(cell("Field cluster one", "Images")).not.toHaveTextContent(DIGEST);
    expect(
      within(cell("Field cluster one", "Last contact")).getByRole("time"),
    ).toBeInTheDocument();
    expect(cell("Hosted control", "Images")).toHaveTextContent("Not reported");
    expect(cell("Hosted control", "Last contact")).toHaveTextContent(
      "No contact recorded",
    );
  });

  it("lists every operator's reports as not checked where the control service is unavailable", async () => {
    hostedAnswering(() => new HttpResponse(null, { status: 503 }));

    await renderList();

    expect(
      await screen.findByText(
        "The control service is unavailable, so its controls are disabled. Teams, people and agents are unaffected.",
      ),
    ).toBeInTheDocument();
    expect(cell("Field cluster one", "Images")).toHaveTextContent(
      "Not checked",
    );
    expect(cell("Hosted control", "Last contact")).toHaveTextContent(
      "Not checked",
    );
  });

  it("leaves the columns out of the list only where the server denies operator:read", async () => {
    hostedAnswering(() => HttpResponse.json({ operators: [reported] }));
    server.use(
      answerPermissionChecks((action) =>
        action === "operator:read" ? "deny" : "allow",
      ),
    );
    const denied = await renderList();
    const headers = () =>
      within(screen.getByRole("table", { name: "Operators" }))
        .getAllByRole("columnheader")
        .map((header) => header.textContent);
    expect(headers()).not.toContain("Images");
    expect(headers()).not.toContain("Last contact");
    // The control: the operators themselves are still listed.
    expect(
      screen.getByRole("link", { name: "Field cluster one" }),
    ).toBeInTheDocument();
    denied.unmount();

    server.use(
      answerPermissionChecks((action) =>
        action === "operator:read" ? "unknown" : "allow",
      ),
    );
    await renderList();
    expect(headers()).toEqual(
      expect.arrayContaining(["Images", "Last contact"]),
    );
  });
});
