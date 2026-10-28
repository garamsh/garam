import { axeViolations, expectHydrated, useTheme } from "./axe";
import { expect, holdSession, test } from "./fixtures";
import { seedMember } from "./seed";

test.describe("projects", () => {
  test("creates a project from the list's dialog and lists it", async ({
    page,
    signedIn,
  }) => {
    await page.goto(`/console/orgs/${signedIn.orgSlug}/projects`);
    const table = page.getByRole("table", { name: "Projects" });
    await expect(
      table.getByText(signedIn.projectName, { exact: true }),
    ).toBeVisible();

    await expectHydrated(page);
    await page.getByRole("button", { name: "Create project" }).click();
    const dialog = page.getByRole("dialog", { name: "Create a project" });
    await dialog.getByLabel("Project name", { exact: true }).fill("Backend");
    await dialog.getByRole("button", { name: "Create project" }).click();

    await expect(dialog).toBeHidden();
    await expect(
      page.getByRole("status").getByText("Created Backend."),
    ).toBeVisible();
    await expect(table.getByText("Backend", { exact: true })).toBeVisible();
  });

  test("offers a member no create action and still lists", async ({
    page,
    context,
    baseURL,
    signedIn,
  }) => {
    const projects = `/console/orgs/${signedIn.orgSlug}/projects`;
    // The control: the owner is offered the action on the same page.
    await page.goto(projects);
    await expect(
      page.getByRole("button", { name: "Create project", exact: true }),
    ).toBeVisible();

    const member = await seedMember(signedIn);
    await context.clearCookies();
    await holdSession(context, baseURL, member.sessionToken);
    await page.goto(projects);

    await expect(
      page
        .getByRole("table", { name: "Projects" })
        .getByText(signedIn.projectName, { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Create project", exact: true }),
    ).toBeHidden();
  });

  for (const theme of ["light", "dark"] as const) {
    test(`passes axe in the ${theme} theme`, async ({ page, signedIn }) => {
      await useTheme(page, theme);
      await page.goto(`/console/orgs/${signedIn.orgSlug}/projects`);
      await expect(page.getByRole("table", { name: "Projects" })).toBeVisible();

      await expectHydrated(page);
      expect(await axeViolations(page)).toEqual([]);
    });
  }

  test("opens a project's page, adds a member, renames it and deletes it by its typed name", async ({
    page,
    signedIn,
  }) => {
    const projects = `/console/orgs/${signedIn.orgSlug}/projects`;
    await page.goto(projects);
    await page
      .getByRole("link", { name: signedIn.projectName, exact: true })
      .click();
    await expect(page).toHaveURL(`${projects}/frontend`);
    await expectHydrated(page);
    expect(await axeViolations(page)).toEqual([]);

    await page
      .getByRole("combobox", { name: "Person or agent", exact: true })
      .click();
    await page
      .getByRole("option", {
        name: `${signedIn.nickname} ${signedIn.userSlug}`,
        exact: true,
      })
      .click();
    await page
      .getByRole("button", { name: "Add to project", exact: true })
      .click();
    await expect(
      page.getByRole("table", { name: `Members of ${signedIn.projectName}` }),
    ).toContainText(signedIn.nickname);

    await page.getByRole("tab", { name: "Settings", exact: true }).click();
    const name = page.getByLabel("Project name", { exact: true });
    await name.fill("Web");
    await page.getByRole("button", { name: "Save name" }).click();
    await expect(page.getByRole("status")).toHaveText("Renamed to Web.");

    await page.reload();
    await expectHydrated(page);
    await page.getByRole("tab", { name: "Settings", exact: true }).click();
    await page
      .getByRole("button", { name: "Delete project", exact: true })
      .click();
    const dialog = page.getByRole("alertdialog", { name: "Delete Web?" });
    const confirm = dialog.getByRole("button", {
      name: "Delete project",
      exact: true,
    });
    await expect(confirm).toBeDisabled();
    await dialog.getByLabel(/to confirm/).fill("Web");
    await confirm.click();

    await expect(page).toHaveURL(projects);
    await expect(page.getByText("Web", { exact: true })).toHaveCount(0);
  });
});
