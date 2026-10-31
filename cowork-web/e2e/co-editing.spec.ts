import { expect, test, type Browser, type Page } from "@playwright/test";

import { seedWorkspace, type Person } from "./seed";

/** A browser of its own, signed in as person. */
async function signedIn(browser: Browser, person: Person): Promise<Page> {
  const context = await browser.newContext();
  await context.addCookies([
    {
      name: "garam_session",
      value: person.sessionToken,
      domain: "cowork.garam.localhost",
      path: "/",
      httpOnly: true,
    },
  ]);
  return context.newPage();
}

const editorOf = (page: Page) =>
  page.getByRole("textbox", { name: "Page content" });

test.describe("co-editing", () => {
  test("two people see each other's typing as they type", async ({
    browser,
  }) => {
    const ws = await seedWorkspace("ada", "bob");
    const [ada, bob] = await Promise.all(
      ws.members.map((m) => signedIn(browser, m)),
    );

    await ada.goto(`/${ws.orgSlug}`);
    await ada.getByRole("textbox", { name: "New page title" }).fill("Plan");
    await ada.getByRole("button", { name: "Add" }).first().click();
    await expect(ada).toHaveURL(new RegExp(`/${ws.orgSlug}/[a-z2-7]{12}$`));
    await bob.goto(ada.url());
    await expect(bob.getByRole("link", { name: "Plan" })).toBeVisible();

    for (const page of [ada, bob]) {
      await expect(page.getByTestId("sync-status")).toHaveText("connected");
    }
    await editorOf(ada).click();
    await ada.keyboard.type("hello from ada");
    await expect(editorOf(bob)).toContainText("hello from ada");

    await editorOf(bob).click();
    await bob.keyboard.press("End");
    await bob.keyboard.type(" and bob");
    await expect(editorOf(ada)).toContainText("hello from ada and bob");

    // A remote edit leaves the local caret where it was: what ada
    // types next lands after ada's own word, before bob's.
    await ada.keyboard.type("!");
    await expect(editorOf(bob)).toContainText("hello from ada! and bob");

    // A block from the insert menu reaches the other browser as the
    // block it is, not as its text. Bob typed last, so Bob's caret is
    // at the end of the text.
    await bob.keyboard.press("Enter");
    await bob.getByRole("button", { name: "Heading 1" }).click();
    await bob.keyboard.type("Decisions");
    await expect(editorOf(ada).locator("h1")).toHaveText("Decisions");
    await expect(editorOf(ada).locator("p").first()).toHaveText(
      "hello from ada! and bob",
    );

    // What both typed was kept: a reload reads it back from the server.
    await ada.reload();
    await expect(editorOf(ada)).toContainText("hello from ada! and bob");
    await expect(editorOf(ada).locator("h1")).toHaveText("Decisions");
  });

  test("someone outside the organization is refused the page", async ({
    browser,
  }) => {
    const ws = await seedWorkspace("ada");
    const [ada] = await Promise.all(
      ws.members.map((m) => signedIn(browser, m)),
    );
    await ada.goto(`/${ws.orgSlug}`);
    await ada.getByRole("textbox", { name: "New page title" }).fill("Private");
    await ada.getByRole("button", { name: "Add" }).first().click();
    await expect(ada).toHaveURL(new RegExp(`/${ws.orgSlug}/[a-z2-7]{12}$`));
    await expect(editorOf(ada)).toBeVisible();

    const outsider = await signedIn(browser, ws.outsider);
    await outsider.goto(ada.url());
    await expect(outsider.getByRole("alert").first()).toContainText(
      "not a member",
    );
    await expect(editorOf(outsider)).toHaveCount(0);
  });
});
