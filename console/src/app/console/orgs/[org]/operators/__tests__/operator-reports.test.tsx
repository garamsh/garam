import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import OperatorPage from "@/app/console/orgs/[org]/operators/[operator]/page";
import OperatorsPage from "@/app/console/orgs/[org]/operators/page";
import { TooltipProvider } from "@/components/ui/tooltip";
import { CONTROL_UNAVAILABLE } from "@/lib/control-refusal-copy";
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

const DIGEST = `sha256:${"ab".repeat(32)}`;

/** A control service as Acme registered it, and the grant it holds. */
type Service = {
  identifier: string;
  name: string;
  endpoint: string;
  operations: string[];
  controllers: string[];
  answer: () => Response | Promise<Response>;
};

const fieldTwo = {
  ...fieldOne,
  grn: "grn:acme:default:operator:field-two",
  name: "Field cluster two",
};
const fieldThree = {
  ...fieldOne,
  grn: "grn:acme:default:operator:field-three",
  name: "Field cluster three",
};

function service(
  overrides: Partial<Service> & { answer: Service["answer"] },
): Service {
  return {
    identifier: "hosted",
    name: "Hosted control",
    endpoint: "https://control.example.test/garam",
    operations: ["operator:read"],
    controllers: [fieldOne.grn],
    ...overrides,
  };
}

/**
 * Acme holding `controllers` beside `services`, each with its grant and
 * its control service answering the reports read; answers what each
 * service was asked, counted by identifier.
 */
function acme(services: Service[], controllers: object[] = [fieldOne]) {
  const asked: Record<string, number> = {};
  server.use(
    http.get(`${API_URL}/orgs/acme/operators`, () =>
      HttpResponse.json([
        ...controllers,
        ...services.map((s) => ({
          ...fieldOne,
          grn: `grn:acme:default:operator:${s.identifier}`,
          name: s.name,
          controlEndpoint: s.endpoint,
        })),
      ]),
    ),
    http.get(`${API_URL}/orgs/acme/operators/field-one/delegation`, () =>
      HttpResponse.json(
        { kind: "not_found", message: "none" },
        { status: 404 },
      ),
    ),
    ...services.flatMap((s) => [
      http.get(
        `${API_URL}/orgs/acme/operators/${s.identifier}/delegation`,
        () =>
          HttpResponse.json({
            grantId: `g-${s.identifier}`,
            operatorGrn: `grn:acme:default:operator:${s.identifier}`,
            controllers: s.controllers,
            operations: s.operations,
            expiresAt: "2099-01-01T00:00:00Z",
            grantedBy: "grn:root:default:user:alice",
            grantedAt: "2026-10-14T00:00:00Z",
          }),
      ),
      http.get(`${s.endpoint}/v1/orgs/acme/operators`, () => {
        asked[s.identifier] = (asked[s.identifier] ?? 0) + 1;
        return s.answer();
      }),
    ]),
  );
  return asked;
}

function answering(...operators: object[]) {
  return () => HttpResponse.json({ operators });
}

/** A report from `grn`: two images, one with a digest. */
function reportFrom(grn: string, overrides: object = {}) {
  return {
    grn,
    images: [
      {
        name: "manager",
        ref: "ghcr.io/garamsh/operator:v0.4.0",
        digest: DIGEST,
      },
      { name: "agent", ref: "ghcr.io/garamsh/gagent:v1.2.0" },
    ],
    reportedAt: "2026-10-14T08:00:00Z",
    lastContactAt: "2026-10-15T09:30:00Z",
    ...overrides,
  };
}

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

function deciding(decision: "deny" | "unknown") {
  server.use(
    answerPermissionChecks((action) =>
      action === "operator:read" ? decision : "allow",
    ),
  );
}

/** The value a fact of the overview's description list holds. */
function fact(term: string) {
  const dt = screen.getByText(term, { selector: "dt" });
  return dt.nextElementSibling as HTMLElement;
}

function headers() {
  return within(screen.getByRole("table", { name: "Operators" }))
    .getAllByRole("columnheader")
    .map((header) => header.textContent);
}

/** The cell of `column` in the row naming `operator`. */
function cell(operator: string, column: string) {
  const table = screen.getByRole("table", { name: "Operators" });
  const row = within(table)
    .getAllByRole("row")
    .find((r) => within(r).queryByRole("link", { name: operator }));
  if (row === undefined) throw new Error(`no row for ${operator}`);
  return within(row).getAllByRole("cell")[headers().indexOf(column)]!;
}

describe("an operator's page: its reported images and last contact", () => {
  it("shows each image by name and reference, its digest shortened and copied whole, when they were reported, and the last contact", async () => {
    acme([service({ answer: answering(reportFrom(fieldOne.grn)) })]);
    const user = userEvent.setup();

    const { container } = await renderOperator();

    const images = fact("Images");
    expect(
      await within(images).findByText("ghcr.io/garamsh/operator:v0.4.0"),
    ).toBeInTheDocument();
    expect(images).toHaveTextContent("ghcr.io/garamsh/gagent:v1.2.0");
    expect(images).toHaveTextContent(`sha256:${"ab".repeat(6)}…`);
    expect(images).not.toHaveTextContent(DIGEST);
    // Only the image that gave a digest shows one.
    expect(
      within(images).getAllByRole("button", { name: /digest$/ }),
    ).toHaveLength(1);
    await user.click(
      within(images).getByRole("button", {
        name: "Copy the manager image's digest",
      }),
    );
    expect(await navigator.clipboard.readText()).toBe(DIGEST);
    await user.click(
      within(images).getByRole("button", {
        name: "Copy the agent image's reference",
      }),
    );
    expect(await navigator.clipboard.readText()).toBe(
      "ghcr.io/garamsh/gagent:v1.2.0",
    );
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

  it("says when the images were reported only where the report says", async () => {
    // The control is the test above, whose report carries the time.
    acme([
      service({
        answer: answering(reportFrom(fieldOne.grn, { reportedAt: undefined })),
      }),
    ]);

    await renderOperator();

    const images = fact("Images");
    expect(
      await within(images).findByText("ghcr.io/garamsh/gagent:v1.2.0"),
    ).toBeInTheDocument();
    expect(within(images).queryByText(/^Reported/)).not.toBeInTheDocument();
  });

  it("says checking while the read is out, and the report once it answers", async () => {
    let release: () => void = () => {};
    const held = new Promise<void>((resolve) => {
      release = resolve;
    });
    acme([
      service({
        answer: async () => {
          await held;
          return HttpResponse.json({ operators: [reportFrom(fieldOne.grn)] });
        },
      }),
    ]);

    await renderOperator();

    expect(
      await within(fact("Images")).findByText("Checking…"),
    ).toBeInTheDocument();
    expect(fact("Last contact")).toHaveTextContent("Checking…");
    // The control: the same read, answered, shows the report.
    release();
    expect(
      await within(fact("Images")).findByText("ghcr.io/garamsh/gagent:v1.2.0"),
    ).toBeInTheDocument();
  });

  it("says not reported, never an empty list, where the service that serves it holds no report or no image", async () => {
    acme([service({ answer: answering() })]);
    const absent = await renderOperator();
    expect(
      await within(fact("Images")).findByText("Not reported"),
    ).toBeInTheDocument();
    expect(fact("Last contact")).toHaveTextContent("No contact recorded");
    absent.unmount();

    acme([
      service({
        answer: answering({
          grn: fieldOne.grn,
          images: [],
          lastContactAt: "2026-10-15T09:30:00Z",
        }),
      }),
    ]);
    await renderOperator();
    expect(
      await within(fact("Images")).findByText("Not reported"),
    ).toBeInTheDocument();
    expect(within(fact("Last contact")).getByRole("time")).toBeInTheDocument();
    expect(within(fact("Images")).queryByRole("list")).not.toBeInTheDocument();
  });

  it("says not checked, in the console's words, where the control service refuses the read", async () => {
    acme([
      service({
        answer: () =>
          HttpResponse.json(
            { message: "operator:read is not delegated to you" },
            { status: 403 },
          ),
      }),
    ]);

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

  it("says not checked, and why, where no grant carries operator:read, and asks no control service", async () => {
    const asked = acme([
      service({
        operations: ["agent-template:read"],
        answer: answering(reportFrom(fieldOne.grn)),
      }),
    ]);

    await renderOperator();

    expect(
      screen.getByText(
        "Hosted control's delegation does not include “Read operator status”.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Open the operator" }),
    ).toHaveAttribute("href", "/console/orgs/acme/operators/hosted");
    expect(fact("Images")).toHaveTextContent("Not checked");
    expect(asked).toEqual({});
  });

  it("says not checked, and why, where garam does not answer for the operators that host a control service", async () => {
    acme([service({ answer: answering(reportFrom(fieldOne.grn)) })]);
    server.use(
      http.get(`${API_URL}/orgs/acme/operators`, () => HttpResponse.error()),
    );

    await renderOperator();

    expect(
      screen.getByText(
        "garam did not answer for this organization's operators, so no control service is reachable from here.",
      ),
    ).toBeInTheDocument();
    expect(fact("Images")).toHaveTextContent("Not checked");
    expect(fact("Last contact")).toHaveTextContent("Not checked");
  });

  it("leaves the facts out and asks nothing only where the server denies operator:read, and shows them where it cannot say", async () => {
    const asked = acme([
      service({ answer: answering(reportFrom(fieldOne.grn)) }),
    ]);
    deciding("deny");
    const denied = await renderOperator();
    expect(
      screen.queryByText("Images", { selector: "dt" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByText("Last contact", { selector: "dt" }),
    ).not.toBeInTheDocument();
    // The control: the rest of the overview is there.
    expect(fact("Description")).toHaveTextContent("Runs the batch agents.");
    expect(asked).toEqual({});
    denied.unmount();

    deciding("unknown");
    await renderOperator();
    expect(
      await within(fact("Images")).findByText("ghcr.io/garamsh/gagent:v1.2.0"),
    ).toBeInTheDocument();
    expect(asked).toEqual({ hosted: 1 });
  });
});

describe("the operators list: each operator's reported images and last contact", () => {
  it("asks every service whose grant carries operator:read and shows each operator's report from the service that serves it", async () => {
    const asked = acme(
      [
        service({ answer: answering(reportFrom(fieldOne.grn)) }),
        service({
          identifier: "hosted-two",
          name: "Second control",
          endpoint: "https://second.example.test",
          controllers: [fieldTwo.grn],
          answer: answering(
            reportFrom(fieldTwo.grn, {
              images: [{ name: "agent", ref: "ghcr.io/garamsh/gagent:v1.3.0" }],
            }),
          ),
        }),
      ],
      [fieldOne, fieldTwo],
    );

    await renderList();

    expect(
      await within(cell("Field cluster two", "Images")).findByText(
        "ghcr.io/garamsh/gagent:v1.3.0",
      ),
    ).toBeInTheDocument();
    expect(cell("Field cluster one", "Images")).toHaveTextContent(
      "ghcr.io/garamsh/gagent:v1.2.0",
    );
    // The list leaves the digest to the operator's page.
    expect(cell("Field cluster one", "Images")).not.toHaveTextContent("sha256");
    expect(asked).toEqual({ hosted: 1, "hosted-two": 1 });
  });

  it("reads an operator a serving service left out as not reported, one no answering service serves as not checked, and a control service as itself", async () => {
    acme(
      [
        service({
          controllers: [fieldOne.grn, fieldTwo.grn],
          answer: answering(reportFrom(fieldOne.grn)),
        }),
        service({
          identifier: "hosted-two",
          name: "Second control",
          endpoint: "https://second.example.test",
          controllers: [fieldThree.grn],
          answer: () => new HttpResponse(null, { status: 503 }),
        }),
      ],
      [fieldOne, fieldTwo, fieldThree],
    );

    await renderList();

    // The control: the operator the answering service reported on.
    expect(
      await within(cell("Field cluster one", "Images")).findByText(
        "ghcr.io/garamsh/gagent:v1.2.0",
      ),
    ).toBeInTheDocument();
    expect(cell("Field cluster two", "Images")).toHaveTextContent(
      "Not reported",
    );
    expect(cell("Field cluster two", "Last contact")).toHaveTextContent(
      "No contact recorded",
    );
    expect(cell("Field cluster three", "Images")).toHaveTextContent(
      "Not checked",
    );
    expect(cell("Hosted control", "Images")).toHaveTextContent(
      "A control service, not a controller",
    );
    expect(cell("Second control", "Last contact")).toHaveTextContent(
      "A control service, not a controller",
    );
    expect(
      screen.getByText(
        "Second control's control service did not answer, so what it reports of the operators it serves was not checked.",
      ),
    ).toBeInTheDocument();
    expect(screen.queryByText(CONTROL_UNAVAILABLE)).not.toBeInTheDocument();
  });

  it("keeps the report heard from later where two services hold one", async () => {
    acme([
      service({
        answer: answering(
          reportFrom(fieldOne.grn, {
            images: [{ name: "agent", ref: "ghcr.io/garamsh/gagent:old" }],
            lastContactAt: "2026-10-01T00:00:00Z",
          }),
        ),
      }),
      service({
        identifier: "hosted-two",
        name: "Second control",
        endpoint: "https://second.example.test",
        answer: answering(
          reportFrom(fieldOne.grn, {
            images: [{ name: "agent", ref: "ghcr.io/garamsh/gagent:new" }],
          }),
        ),
      }),
    ]);

    await renderList();

    expect(
      await within(cell("Field cluster one", "Images")).findByText(
        "ghcr.io/garamsh/gagent:new",
      ),
    ).toBeInTheDocument();
    expect(cell("Field cluster one", "Images")).not.toHaveTextContent(
      "gagent:old",
    );
  });

  it("with no control service registered, shows no notice and marks each operator not checked", async () => {
    acme([]);

    await renderList();

    expect(cell("Field cluster one", "Images")).toHaveTextContent(
      "Not checked",
    );
    expect(cell("Field cluster one", "Last contact")).toHaveTextContent(
      "Not checked",
    );
    expect(
      screen.queryByText("The control service can't be used here"),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: "Open operators" }),
    ).not.toBeInTheDocument();
  });

  it("asks no service where every operator is a control service, and asks once there is a controller", async () => {
    const asked = acme([service({ controllers: [], answer: answering() })], []);
    const alone = await renderList();
    expect(cell("Hosted control", "Images")).toHaveTextContent(
      "A control service, not a controller",
    );
    expect(asked).toEqual({});
    alone.unmount();

    // The control: the same service, with a controller to report on.
    const again = acme([service({ answer: answering() })]);
    await renderList();
    expect(
      await within(cell("Field cluster one", "Images")).findByText(
        "Not reported",
      ),
    ).toBeInTheDocument();
    expect(again).toEqual({ hosted: 1 });
  });

  it("leaves the columns out and asks nothing only where the server denies operator:read", async () => {
    const asked = acme([
      service({ answer: answering(reportFrom(fieldOne.grn)) }),
    ]);
    deciding("deny");
    const denied = await renderList();
    expect(headers()).not.toContain("Images");
    expect(headers()).not.toContain("Last contact");
    // The control: the operators themselves are still listed.
    expect(
      screen.getByRole("link", { name: "Field cluster one" }),
    ).toBeInTheDocument();
    expect(asked).toEqual({});
    denied.unmount();

    deciding("unknown");
    await renderList();
    expect(headers()).toEqual(
      expect.arrayContaining(["Images", "Last contact"]),
    );
  });
});
