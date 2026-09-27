import { expect, test } from "./fixtures";

test.describe("operators", () => {
  test("registers a named operator, renames it and defines an agent against it", async ({
    page,
    signedIn,
  }) => {
    const operators = `/console/orgs/${signedIn.orgSlug}/operators`;
    await page.goto(operators);
    await expect(
      page.getByRole("link", { name: signedIn.operatorName, exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText(signedIn.operatorIdentifier, { exact: true }),
    ).toBeVisible();

    // The fields are disabled until React hydrates, so waiting for one
    // to be enabled waits on the form being able to hold what is typed.
    const identifier = page.getByLabel("Identifier", { exact: true });
    await expect(identifier).toBeEnabled();
    await identifier.fill("Field-Two");
    await expect(
      page.getByText(
        "An operator identifier holds only lowercase letters, digits and hyphens.",
        { exact: true },
      ),
    ).toBeVisible();
    const register = page.getByRole("button", {
      name: "Register operator",
      exact: true,
    });
    await expect(register).toBeDisabled();

    await identifier.fill("field-two");
    await page.getByLabel("Name", { exact: true }).fill("Field two");
    await page
      .getByLabel("Description", { exact: true })
      .fill("Runs the batch agents.");
    await register.click();
    await expect(
      page.getByRole("heading", { name: "Registered Field two", exact: true }),
    ).toBeVisible();

    // The token the operator enrols with is shown once, beside the
    // sentence saying so (ADR-0064). Substring, because the sentence is
    // longer than the claim under test.
    await expect(
      page.getByLabel("Enrollment token", { exact: true }),
    ).toHaveValue(/^[0-9a-f]{48}$/);
    await expect(
      page.getByText("This is the only time this token is shown"),
    ).toBeVisible();

    await page
      .getByRole("link", { name: "Open operator", exact: true })
      .click();
    await expect(page).toHaveURL(`${operators}/field-two`);
    await expect(
      page.getByRole("heading", { level: 1, name: "Field two", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("Enrollment token available", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("Unknown — no report", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByLabel("Enrollment token", { exact: true }),
    ).toBeHidden();

    // Renaming keeps the identifier the page is addressed by.
    const name = page.getByLabel("Name", { exact: true });
    await expect(name).toBeEnabled();
    await name.fill("Staging cluster");
    await page
      .getByRole("button", { name: "Save details", exact: true })
      .click();
    await expect(
      page.getByText("Saved as Staging cluster.", { exact: true }),
    ).toBeVisible();
    await page.reload();
    await expect(
      page.getByRole("heading", {
        level: 1,
        name: "Staging cluster",
        exact: true,
      }),
    ).toBeVisible();

    await page.goto(`/console/orgs/${signedIn.orgSlug}/agents`);
    const operator = page.getByLabel("Operator", { exact: true });
    await expect(operator).toBeEnabled();
    await operator.selectOption({ label: "Staging cluster (field-two)" });
    await page
      .getByRole("button", { name: "Define agent", exact: true })
      .click();
    const minted = page.getByLabel("Agent identifier", { exact: true });
    await expect(minted).toBeVisible();
    const agent = await minted.inputValue();

    // The definition now refuses the delete, and the operator's page
    // lists it beside the route to the definitions.
    await page.goto(`${operators}/field-two`);
    await expect(page.getByText(agent, { exact: true })).toBeVisible();
    await expect(
      page.getByRole("link", { name: "View in Agents", exact: true }),
    ).toHaveAttribute(
      "href",
      `/console/orgs/${signedIn.orgSlug}/agents#definitions`,
    );
    await page
      .getByRole("button", { name: "Delete operator", exact: true })
      .click();
    await page
      .getByRole("button", { name: "Confirm deletion", exact: true })
      .click();
    // Substring: the refusal goes on to say what to do.
    await expect(
      page.getByText("an agent definition names this operator"),
    ).toBeVisible();
    await expect(page).toHaveURL(`${operators}/field-two`);
  });

  test("opens, renames and deletes an operator registered as new", async ({
    page,
    signedIn,
  }) => {
    // `new` is a valid identifier, so it is an operator's page and no
    // create route may take it (ADR-0074).
    const operators = `/console/orgs/${signedIn.orgSlug}/operators`;
    await page.goto(operators);
    const identifier = page.getByLabel("Identifier", { exact: true });
    await expect(identifier).toBeEnabled();
    await identifier.fill("new");
    await page
      .getByRole("button", { name: "Register operator", exact: true })
      .click();
    await expect(
      page.getByRole("heading", { name: "Registered new", exact: true }),
    ).toBeVisible();

    await page.goto(`${operators}/new`);
    await expect(
      page.getByRole("heading", { level: 1, name: "new", exact: true }),
    ).toBeVisible();
    const name = page.getByLabel("Name", { exact: true });
    await expect(name).toBeEnabled();
    await name.fill("New cluster");
    await page
      .getByRole("button", { name: "Save details", exact: true })
      .click();
    await expect(
      page.getByText("Saved as New cluster.", { exact: true }),
    ).toBeVisible();

    await page
      .getByRole("button", { name: "Delete operator", exact: true })
      .click();
    await page
      .getByRole("button", { name: "Confirm deletion", exact: true })
      .click();
    await expect(page).toHaveURL(operators);
    await expect(
      page.getByRole("link", { name: "New cluster", exact: true }),
    ).toBeHidden();
    // The control: the seeded operator is still listed, so the absence
    // above is the delete and not an empty page.
    await expect(
      page.getByRole("link", { name: signedIn.operatorName, exact: true }),
    ).toBeVisible();
  });

  test("deletes an operator no definition names and returns to the list", async ({
    page,
    signedIn,
  }) => {
    const operators = `/console/orgs/${signedIn.orgSlug}/operators`;
    await page.goto(`${operators}/${signedIn.operatorIdentifier}`);
    await expect(
      page.getByText("No agent definition names this operator.", {
        exact: true,
      }),
    ).toBeVisible();

    await page
      .getByRole("button", { name: "Delete operator", exact: true })
      .click();
    await page
      .getByRole("button", { name: "Confirm deletion", exact: true })
      .click();

    await expect(page).toHaveURL(operators);
    await expect(
      page.getByText("This organization has registered no operators.", {
        exact: true,
      }),
    ).toBeVisible();
  });
});
