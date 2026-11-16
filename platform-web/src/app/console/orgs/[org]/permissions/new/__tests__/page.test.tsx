import { render, screen, waitFor } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { Component, type ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import { CreatePolicyForm } from "@/app/console/orgs/[org]/permissions/new/_components/create-policy-form";
import NewPolicyPage from "@/app/console/orgs/[org]/permissions/new/page";
import { roleLinkChoices, type RoleLinkSources } from "@/features/orgs";
import {
  API_URL,
  CREATED_POLICY_ID,
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
const CEDAR = "forbid (principal, action, resource);";
const OPENED = `/console/orgs/acme/permissions/${CREATED_POLICY_ID}`;

/**
 * Stands where Next's router stands: a created policy is the page the
 * action redirects to, so a redirect is shown as the page it opens.
 */
class Router extends Component<
  { children: ReactNode },
  { opened: string | null }
> {
  state = { opened: null as string | null };
  static getDerivedStateFromError(error: Error) {
    return { opened: error.message.replace("NEXT_REDIRECT:", "") };
  }
  render() {
    return this.state.opened === null ? (
      this.props.children
    ) : (
      <p>Opened {this.state.opened}</p>
    );
  }
}

function renderNewPolicy() {
  return renderServerComponent(
    NewPolicyPage({ params: Promise.resolve({ org: "acme" }) }),
  );
}

const HELD: RoleLinkSources = {
  teams: [engineering],
  agents: [reportedAgent],
  members: [aliceMembership],
  projects: [frontend],
};

/** The form over what the mock holds, or over the given lists. */
function renderForm(sources: RoleLinkSources = HELD) {
  return render(
    <Router>
      <CreatePolicyForm
        org="acme"
        orgGrn={ORG}
        offered={["guardrail", "permit", "role-link"]}
        choices={roleLinkChoices(sources)}
      />
    </Router>,
  );
}

/** Chooses `option` in the select labelled `label`. */
async function pick(user: UserEvent, label: string, option: string) {
  await user.click(screen.getByRole("combobox", { name: label }));
  await user.click(await screen.findByRole("option", { name: option }));
}

/** The options the select labelled `label` offers, as written. */
async function offeredBy(user: UserEvent, label: string) {
  await user.click(screen.getByRole("combobox", { name: label }));
  const options = await screen.findAllByRole("option");
  const names = options.map((option) => option.textContent);
  await user.keyboard("{Escape}");
  return names;
}

/** Fills the role form with a team over the organization, and sends. */
async function sendTeamLink(user: UserEvent) {
  await user.click(screen.getByRole("radio", { name: /Role link/ }));
  await user.type(screen.getByLabelText("Name"), "Eng admins");
  await pick(user, "Role", "admin");
  await pick(user, "Team or agent", "Engineering (eng)");
  await user.click(screen.getByRole("button", { name: "Create policy" }));
}

/** What the service answers, as the next answer to a create. */
function answerCreate(status: number, kind: string, message: string) {
  server.use(
    http.post(POLICIES, () => HttpResponse.json({ kind, message }, { status })),
  );
}

describe("new policy page", () => {
  it("offers the form for the kinds the server does not deny", async () => {
    server.use(
      answerPermissionChecks((action) =>
        action === "iam:permit.write" ? "deny" : "allow",
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
    // The control: a caller denied nothing is offered the form.
    const allowed = await renderNewPolicy();
    expect(screen.getByLabelText("Name")).toBeInTheDocument();
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
    allowed.unmount();

    server.use(answerPermissionChecks(() => "deny"));
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

    expect(await offeredBy(user, "Team or agent")).toEqual([
      "Engineering (eng)",
      "1c3d5e7f90a2b4c6",
    ]);
    // The control: a person is offered where the platform takes one.
    await user.click(
      screen.getByRole("radio", { name: /A person over a team or a project/ }),
    );
    expect(await offeredBy(user, "Person")).toEqual(["Alice (alice)"]);
  });

  it("offers a team or a project as the resource of a person's link", async () => {
    const user = userEvent.setup();
    renderForm();
    await user.click(screen.getByRole("radio", { name: /Role link/ }));
    await user.click(
      screen.getByRole("radio", { name: /A person over a team or a project/ }),
    );

    expect(await offeredBy(user, "Team or project")).toEqual([
      "Engineering (eng)",
      expect.stringContaining("frontend"),
    ]);
  });

  it("says a list garam refused is not listed, never that the organization holds none", async () => {
    // The control: a list that was read and is empty says so.
    const user = userEvent.setup();
    const empty = renderForm({ ...HELD, teams: [], agents: [] });
    await user.click(screen.getByRole("radio", { name: /Role link/ }));
    expect(
      screen.getByText("The organization holds no team or agent."),
    ).toBeVisible();
    expect(screen.queryByText(/not listed to you/)).not.toBeInTheDocument();
    empty.unmount();

    renderForm({ ...HELD, teams: null, agents: null });
    await user.click(screen.getByRole("radio", { name: /Role link/ }));

    expect(
      screen.getByText(
        "This organization's teams and agents are not listed to you, so none of them can be chosen here.",
      ),
    ).toBeVisible();
    expect(
      screen.queryByText(/The organization holds no/),
    ).not.toBeInTheDocument();
  });

  it("says a link the platform refuses at the form, keeps the choices, and opens the link once it is accepted", async () => {
    answerCreate(400, "invalid_argument", "that link is not allowed");
    const user = userEvent.setup();
    renderForm();
    await sendTeamLink(user);

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "that link is not allowed",
    );
    expect(screen.getByLabelText("Name")).toHaveValue("Eng admins");
    expect(screen.getByRole("combobox", { name: "Role" })).toHaveTextContent(
      "admin",
    );
    expect(
      screen.getByRole("combobox", { name: "Team or agent" }),
    ).toHaveTextContent("Engineering (eng)");

    // The control: the choices kept are accepted as they stand.
    server.resetHandlers();
    await user.click(screen.getByRole("button", { name: "Create policy" }));
    expect(await screen.findByText(`Opened ${OPENED}`)).toBeVisible();
  });

  it.each([
    [
      403,
      "permission_denied",
      "You can't write a role link in this organization.",
    ],
    [409, "conflict", "The policy service already holds this role link."],
  ])(
    "says a %i at the form and opens the link once it is accepted",
    async (status, kind, said) => {
      answerCreate(status, kind, "refused");
      const user = userEvent.setup();
      renderForm();
      await sendTeamLink(user);

      expect(await screen.findByRole("alert")).toHaveTextContent(said);

      // The control: the same link, accepted.
      server.resetHandlers();
      await user.click(screen.getByRole("button", { name: "Create policy" }));
      expect(await screen.findByText(`Opened ${OPENED}`)).toBeVisible();
    },
  );
});

describe("create policy form, the Cedar editor", () => {
  it("says the service's message at the editor, keeps the text, and opens the policy once the text parses", async () => {
    const user = userEvent.setup();
    renderForm();
    await user.type(screen.getByLabelText("Name"), "Broken");
    const text = screen.getByLabelText("Cedar text");
    await user.type(text, "not cedar");

    await user.click(screen.getByRole("button", { name: "Create policy" }));

    await waitFor(() => expect(text).toHaveAttribute("aria-invalid", "true"));
    expect(text).toHaveAccessibleDescription(
      expect.stringContaining("unexpected token at line 1"),
    );
    expect(text).toHaveValue("not cedar");
    expect(screen.getByLabelText("Name")).toHaveValue("Broken");

    // The control: Cedar the service parses is stored.
    await user.clear(text);
    await user.type(text, CEDAR);
    await user.click(screen.getByRole("button", { name: "Create policy" }));
    expect(await screen.findByText(`Opened ${OPENED}`)).toBeVisible();
  });

  it("names the missing name at the name field, and opens the policy once it is named", async () => {
    const user = userEvent.setup();
    renderForm();
    await user.type(screen.getByLabelText("Cedar text"), CEDAR);
    const name = screen.getByLabelText("Name");
    await user.type(name, "   ");

    // The browser's own `required` holds back an empty name; whitespace
    // reaches the action, which names it.
    await user.click(screen.getByRole("button", { name: "Create policy" }));

    await waitFor(() => expect(name).toHaveAttribute("aria-invalid", "true"));
    expect(name).toHaveAccessibleDescription("Name the policy.");

    // The control: a name is accepted.
    await user.type(name, "Deny all");
    await user.click(screen.getByRole("button", { name: "Create policy" }));
    expect(await screen.findByText(`Opened ${OPENED}`)).toBeVisible();
  });
});
