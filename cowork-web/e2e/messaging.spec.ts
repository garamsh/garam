import { expect, test, type Browser, type Page } from "@playwright/test";

import { seedWorkspace, type Person } from "./seed";

async function signedIn(browser: Browser, person: Person): Promise<Page> {
  const context = await browser.newContext();
  await context.addCookies([
    {
      name: "garam_session",
      value: person.sessionToken,
      domain: "localhost",
      path: "/",
      httpOnly: true,
    },
  ]);
  return context.newPage();
}

/** Creates a channel as page's person and answers its URL. */
async function createChannel(
  page: Page,
  org: string,
  name: string,
  kind: "public" | "private" = "public",
) {
  await page.goto(`/${org}/channels`);
  await page.getByRole("textbox", { name: "Channel name" }).fill(name);
  await page.getByRole("combobox", { name: "Who may join" }).selectOption(kind);
  await page.getByRole("button", { name: "Create" }).click();
  await expect(page).toHaveURL(new RegExp(`/${org}/channels/[a-z2-7]{12}$`));
  return page.url();
}

const messages = (page: Page) => page.getByRole("list", { name: "Messages" });

test.describe("messaging", () => {
  test("a message reaches the other browser as it is posted, and a reply joins its thread", async ({
    browser,
  }) => {
    const ws = await seedWorkspace("ada", "bob");
    const [ada, bob] = await Promise.all(
      ws.members.map((m) => signedIn(browser, m)),
    );
    const url = await createChannel(ada, ws.orgSlug, "general");
    await bob.goto(url);
    for (const page of [ada, bob]) {
      await expect(page.getByTestId("live-status").first()).toHaveText("live");
    }

    await ada.getByRole("textbox", { name: "Message" }).fill("hello bob");
    await ada.locator("summary", { hasText: "Mention" }).click();
    await ada.getByRole("checkbox", { name: "bob" }).check();
    await ada.getByRole("button", { name: "Send" }).click();
    await expect(messages(bob)).toContainText("hello bob");
    await expect(messages(bob)).toContainText("@bob");

    await messages(bob).getByRole("link", { name: "Reply" }).click();
    const thread = bob.getByRole("complementary", { name: "Thread panel" });
    await thread.getByRole("textbox", { name: "Reply" }).fill("hi ada");
    await thread.getByRole("button", { name: "Send" }).click();
    await expect(thread.getByRole("list", { name: "Thread" })).toContainText(
      "hi ada",
    );
    await expect(
      messages(ada).getByRole("link", { name: "1 reply" }),
    ).toBeVisible();

    await bob.goto(`/${ws.orgSlug}/search?q=hello`);
    await expect(bob.getByRole("list", { name: "Results" })).toContainText(
      "hello bob",
    );
  });

  test("a message links a page, and the page lists the message", async ({
    browser,
  }) => {
    const ws = await seedWorkspace("ada");
    const [ada] = await Promise.all(
      ws.members.map((m) => signedIn(browser, m)),
    );
    await ada.goto(`/${ws.orgSlug}`);
    await ada.getByRole("textbox", { name: "New page title" }).fill("Spec");
    await ada.getByRole("button", { name: "Add" }).first().click();
    await expect(ada).toHaveURL(new RegExp(`/${ws.orgSlug}/[a-z2-7]{12}$`));
    const pageUrl = ada.url();

    await createChannel(ada, ws.orgSlug, "general");
    await ada.getByRole("textbox", { name: "Message" }).fill("read the spec");
    await ada
      .getByRole("combobox", { name: "Link a page" })
      .selectOption({ label: "Spec" });
    await ada.getByRole("button", { name: "Send" }).click();
    await messages(ada).getByRole("link", { name: "Spec" }).click();
    await expect(ada).toHaveURL(pageUrl);
    await expect(
      ada.getByRole("region", { name: "Linked from messages" }),
    ).toContainText("read the spec");
  });

  test("a private channel is closed to a member not on it", async ({
    browser,
  }) => {
    const ws = await seedWorkspace("ada", "bob");
    const [ada, bob] = await Promise.all(
      ws.members.map((m) => signedIn(browser, m)),
    );
    const url = await createChannel(ada, ws.orgSlug, "plans", "private");
    await ada.getByRole("textbox", { name: "Message" }).fill("only us");
    await ada.getByRole("button", { name: "Send" }).click();
    await expect(messages(ada)).toContainText("only us");

    await bob.goto(url);
    await expect(bob.getByRole("alert").first()).toContainText(
      "not a participant",
    );
    await expect(bob.getByText("only us")).toHaveCount(0);
  });
});
