import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { CreatePolicyForm } from "@/app/console/orgs/[org]/permissions/new/_components/create-policy-form";
import NewPolicyPage from "@/app/console/orgs/[org]/permissions/new/page";
import { roleLinkChoices } from "@/features/orgs";
import {
  API_URL,
  aliceMembership,
  answerPermissionChecks,
  engineering,
  frontend,
  reportedAgent,
} from "@/testing/mocks/handlers";
import { renderServerComponent } from "@/testing/render";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
  usePathname: () => "/console/orgs/acme/permissions/new",
}));
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const POLICIES = `${API_URL}/orgs/acme/iam-policies`;
const ORG = "grn:root/iam/org/acme";
/** The one action the platform answers on the organization for a link. */
const ON_ORG = ["iam:policy.attach"];

function renderNewPolicy() {
  return renderServerComponent(
    NewPolicyPage({ params: Promise.resolve({ org: "acme" }) }),
  );
}

/** The form over what the mock organization holds. */
function renderForm(offered = ["guardrail", "permit", "role-link"] as const) {
  return render(
    <CreatePolicyForm
      org="acme"
      orgGrn={ORG}
      offered={offered}
      choices={roleLinkChoices({
        teams: [engineering],
        agents: [reportedAgent],
        members: [aliceMembership],
        projects: [frontend],
      })}
    />,
  );
}

/** The labels of the options a select offers, without its empty one. */
function optionsOf(select: HTMLElement) {
  return within(select)
    .getAllByRole("option")
    .map((option) => option.textContent)
    .filter((label) => label !== "Choose…");
}

describe("new policy page", () => {
  it("offers the form for the kinds the server does not deny", async () => {
    server.use(
      answerPermissionChecks(
        (action) => (action === "iam:permit.write" ? "deny" : "allow"),
        ON_ORG,
      ),
    );

    await renderNewPolicy();

    expect(screen.getByRole("radio", { name: /Guardrail/ })).toBeChecked();
    expect(screen.getByRole("radio", { name: /Role link/ })).toBeVisible();
    // The control: the kind denied is the only one withdrawn.
    expect(
      screen.queryByRole("radio", { name: /Permit/ }),
    ).not.toBeInTheDocument();
  });

  it("asks iam:policy.create, iam:permit.write and iam:policy.attach on the organization", async () => {
    const asked: unknown[] = [];
    server.use(
      http.post(
        `${API_URL}/orgs/acme/permission-checks`,
        async ({ request }) => {
          const { checks } = (await request.json()) as {
            checks: { action: string; resource?: unknown }[];
          };
          asked.push(...checks);
          return HttpResponse.json({
            results: checks.map((check) => ({ ...check, decision: "allow" })),
          });
        },
      ),
    );

    await renderNewPolicy();

    expect(asked).toEqual(
      expect.arrayContaining([
        { action: "iam:policy.create" },
        { action: "iam:permit.write" },
        { action: "iam:policy.attach" },
      ]),
    );
  });

  it("says a caller denied every kind cannot create a policy instead of a form", async () => {
    server.use(answerPermissionChecks(() => "deny", ON_ORG));

    await renderNewPolicy();

    expect(screen.getByRole("status")).toHaveTextContent(
      "No policy lets you create policies",
    );
    expect(screen.queryByLabelText("Name")).not.toBeInTheDocument();
  });

  it("offers every kind when the check could not be asked", async () => {
    server.use(
      http.post(`${API_URL}/orgs/acme/permission-checks`, () =>
        HttpResponse.error(),
      ),
    );

    await renderNewPolicy();

    expect(screen.getByRole("radio", { name: /Permit/ })).toBeVisible();
    expect(screen.getByRole("radio", { name: /Role link/ })).toBeVisible();
  });
});

describe("create policy form, the role form", () => {
  it("offers a team or an agent over the organization, and no person", async () => {
    const user = userEvent.setup();
    renderForm();
    await user.click(screen.getByRole("radio", { name: /Role link/ }));

    const principal = screen.getByLabelText("Team or agent");
    expect(optionsOf(principal)).toEqual([
      "Engineering (eng)",
      "1c3d5e7f90a2b4c6",
    ]);
    // The control: a person is offered where the platform takes one.
    await user.click(
      screen.getByRole("radio", { name: /A person over a team or a project/ }),
    );
    expect(optionsOf(screen.getByLabelText("Person"))).toEqual([
      `Alice (${"alice"})`,
    ]);
    expect(screen.getByLabelText("Team or project")).toBeInTheDocument();
  });

  it("offers a team or a project as the resource of a person's link", async () => {
    const user = userEvent.setup();
    renderForm();
    await user.click(screen.getByRole("radio", { name: /Role link/ }));
    await user.click(
      screen.getByRole("radio", { name: /A person over a team or a project/ }),
    );

    expect(optionsOf(screen.getByLabelText("Team or project"))).toEqual([
      "Engineering (eng)",
      expect.stringContaining("frontend"),
    ]);
  });

  it("says a link the platform refuses at the form, and keeps the choices", async () => {
    server.use(
      http.post(POLICIES, () =>
        HttpResponse.json(
          { kind: "invalid_argument", message: "that link is not allowed" },
          { status: 400 },
        ),
      ),
    );
    const user = userEvent.setup();
    renderForm();
    await user.click(screen.getByRole("radio", { name: /Role link/ }));
    await user.type(screen.getByLabelText("Name"), "Eng admins");
    await user.selectOptions(screen.getByLabelText("Role"), "admin");
    await user.selectOptions(
      screen.getByLabelText("Team or agent"),
      engineering.grn,
    );
    await user.click(screen.getByRole("button", { name: "Create policy" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "that link is not allowed",
    );
    expect(screen.getByLabelText("Name")).toHaveValue("Eng admins");
    expect(screen.getByLabelText("Role")).toHaveValue("admin");
  });

  it.each([
    [
      403,
      "permission_denied",
      "You can't write a role link in this organization.",
    ],
    [409, "conflict", "The policy service already holds this role link."],
  ])("says a %i at the form", async (status, kind, said) => {
    server.use(
      http.post(POLICIES, () =>
        HttpResponse.json({ kind, message: "refused" }, { status }),
      ),
    );
    const user = userEvent.setup();
    renderForm();
    await user.click(screen.getByRole("radio", { name: /Role link/ }));
    await user.type(screen.getByLabelText("Name"), "Eng admins");
    await user.selectOptions(screen.getByLabelText("Role"), "admin");
    await user.selectOptions(
      screen.getByLabelText("Team or agent"),
      engineering.grn,
    );
    await user.click(screen.getByRole("button", { name: "Create policy" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(said);
  });
});

describe("create policy form, the Cedar editor", () => {
  it("says the service's message at the editor and keeps the text", async () => {
    const user = userEvent.setup();
    renderForm();
    await user.type(screen.getByLabelText("Name"), "Broken");
    const text = screen.getByLabelText("Cedar text");
    await user.type(text, "not cedar");
    // The control: nothing is refused before the service answers.
    expect(text).not.toHaveAttribute("aria-invalid", "true");

    await user.click(screen.getByRole("button", { name: "Create policy" }));

    await waitFor(() => expect(text).toHaveAttribute("aria-invalid", "true"));
    expect(text).toHaveAccessibleDescription(
      expect.stringContaining("unexpected token at line 1"),
    );
    expect(text).toHaveValue("not cedar");
    expect(screen.getByLabelText("Name")).toHaveValue("Broken");
  });

  it("names the missing name at the name field", async () => {
    const user = userEvent.setup();
    renderForm();
    await user.type(
      screen.getByLabelText("Cedar text"),
      "forbid (principal, action, resource);",
    );
    const name = screen.getByLabelText("Name");
    await user.type(name, "   ");

    // The browser's own `required` holds back an empty name; whitespace
    // reaches the action, which names it.
    await user.click(screen.getByRole("button", { name: "Create policy" }));

    await waitFor(() => expect(name).toHaveAttribute("aria-invalid", "true"));
    expect(name).toHaveAccessibleDescription("Name the policy.");
  });
});
