import { render, screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";
import { z } from "zod";

import { ActivatePolicyForm } from "@/app/console/orgs/[org]/permissions/[policy]/_components/activate-policy-form";
import { MakeDefaultForm } from "@/app/console/orgs/[org]/permissions/[policy]/_components/make-default-form";
import { API_URL, baselineRead, denyDeletes } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";
import { setupUser } from "@/testing/user";

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const BASELINE = `${API_URL}/orgs/acme/policies/0a1b2c3d4e5f6071`;
const DENY = `${API_URL}/orgs/acme/policies/d1e2f3a4b5c6d7e8`;
const EVERYONE = "No attachments: applies to everyone in this organization.";

describe("activate policy form", () => {
  it("states the version and the scope, and asks to confirm everyone", async () => {
    const activated: string[] = [];
    server.use(
      http.post(`${BASELINE}/activate`, () => {
        activated.push("baseline");
        return HttpResponse.json(baselineRead);
      }),
    );
    const user = setupUser();
    render(
      <ActivatePolicyForm
        org="acme"
        policy="0a1b2c3d4e5f6071"
        policyName="Baseline read"
        version={1}
        reach={EVERYONE}
        orgWide
        conditioned={false}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Activate…" }));

    expect(
      screen.getByText(`Activate Baseline read with version 1? ${EVERYONE}`),
    ).toBeInTheDocument();
    const confirm = screen.getByRole("checkbox", {
      name: "Apply it to everyone in this organization",
    });
    expect(confirm).toBeRequired();
    await user.click(confirm);
    await user.click(
      screen.getByRole("button", { name: "Activate for everyone" }),
    );
    await vi.waitFor(() => expect(activated).toEqual(["baseline"]));
  });

  it("asks nothing more of an attached policy", async () => {
    const user = setupUser();
    render(
      <ActivatePolicyForm
        org="acme"
        policy="d1e2f3a4b5c6d7e8"
        policyName="Deny project deletion"
        version={1}
        reach="Applies to Engineering (Team)."
        orgWide={false}
        conditioned={false}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Activate…" }));

    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Activate" }),
    ).toBeInTheDocument();
  });
});

describe("confirmations and Conditions", () => {
  it("says the version to evaluate carries a Condition that restricts nothing", async () => {
    const user = setupUser();
    const props = {
      org: "acme",
      policy: "d1e2f3a4b5c6d7e8",
      current: 1,
      target: 2,
      active: true,
      reach: "Applies to Engineering (Team).",
      orgWide: false,
    };
    // The control: without a Condition, the confirmation carries no
    // such notice.
    const plain = render(<MakeDefaultForm {...props} conditioned={false} />);
    await user.click(screen.getByRole("button", { name: "Make default" }));
    expect(screen.queryByRole("note")).not.toBeInTheDocument();
    plain.unmount();

    render(<MakeDefaultForm {...props} conditioned />);
    await user.click(screen.getByRole("button", { name: "Make default" }));

    expect(screen.getByRole("note")).toHaveTextContent(
      "This document has a Condition. garam does not evaluate Conditions, so the statement applies without it.",
    );
  });
});

describe("make default form", () => {
  it("names both versions and posts the chosen one, older or newer", async () => {
    const Rollback = z.strictObject({ version: z.number() });
    const posted: z.infer<typeof Rollback>[] = [];
    server.use(
      http.post(`${DENY}/rollback`, async ({ request }) => {
        posted.push(Rollback.parse(await request.json()));
        return HttpResponse.json(denyDeletes);
      }),
    );
    const user = setupUser();
    render(
      <MakeDefaultForm
        org="acme"
        policy="d1e2f3a4b5c6d7e8"
        current={2}
        target={1}
        active
        reach="Applies to Engineering (Team)."
        orgWide={false}
        conditioned={false}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Roll back to v1" }));
    expect(
      screen.getByText(
        "Evaluate version 1 instead of version 2? This applies at once. Applies to Engineering (Team).",
      ),
    ).toBeInTheDocument();
    await user.click(
      screen.getByRole("button", { name: "Roll back to version 1" }),
    );

    await vi.waitFor(() => expect(posted).toEqual([{ version: 1 }]));
  });

  it("carries the everyone confirmation for an active policy with no attachment", async () => {
    const user = setupUser();
    const { unmount } = render(
      <MakeDefaultForm
        org="acme"
        policy="0a1b2c3d4e5f6071"
        current={1}
        target={2}
        active
        reach={EVERYONE}
        orgWide
        conditioned={false}
      />,
    );
    await user.click(screen.getByRole("button", { name: "Make default" }));
    expect(
      screen.getByRole("checkbox", {
        name: "Apply it to everyone in this organization",
      }),
    ).toBeRequired();
    unmount();

    // The control: the same change on a draft evaluates nothing, so it
    // carries no such confirmation.
    render(
      <MakeDefaultForm
        org="acme"
        policy="0a1b2c3d4e5f6071"
        current={1}
        target={2}
        active={false}
        reach={EVERYONE}
        orgWide
        conditioned={false}
      />,
    );
    await user.click(screen.getByRole("button", { name: "Make default" }));
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
    expect(
      screen.getByText(
        "Evaluate version 2 instead of version 1? Nothing changes until the policy is activated.",
      ),
    ).toBeInTheDocument();
  });
});
