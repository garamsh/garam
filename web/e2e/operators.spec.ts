import { expect, test } from "./fixtures";
import { seedDefinition } from "./seed";

test.describe("operators", () => {
  test("registers a named operator, renames it and lists the definition naming it", async ({
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
    // So is the credential the operator exchanges for tokens (ADR-0100).
    const credential = page.getByLabel("Credential", { exact: true });
    await expect(credential).toHaveValue(/^garam_oc_[0-9a-f]{64}$/);
    const issued = await credential.inputValue();

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

    // Re-issuing answers a credential other than the one registration
    // answered, once, after the confirmation.
    await page
      .getByRole("button", { name: "Re-issue credential", exact: true })
      .click();
    await page
      .getByRole("button", { name: "Confirm re-issue", exact: true })
      .click();
    const reissued = page.getByLabel("Credential", { exact: true });
    await expect(reissued).toHaveValue(/^garam_oc_[0-9a-f]{64}$/);
    expect(await reissued.inputValue()).not.toBe(issued);

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

    // A definition kept from before garam stopped accepting them
    // (ADR-0090) names the operator.
    const agent = await seedDefinition(signedIn, "Batch runner", "field-two");

    // The definition refuses the delete, and the operator's page lists
    // it by name beside the route to the definitions.
    await page.goto(`${operators}/field-two`);
    await expect(page.getByText(agent, { exact: true })).toBeVisible();
    await expect(
      page.getByRole("link", { name: "Batch runner", exact: true }),
    ).toHaveAttribute(
      "href",
      `/console/orgs/${signedIn.orgSlug}/agents/${agent}`,
    );
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

    // The control: the seeded operator, which no definition names, is
    // deleted by the same controls, so what refused the one above is
    // its definition and not a delete that is refused for everyone.
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
      page.getByRole("link", { name: signedIn.operatorName, exact: true }),
    ).toBeHidden();
    await expect(
      page.getByRole("link", { name: "Staging cluster", exact: true }),
    ).toBeVisible();
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
});
