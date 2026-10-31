import { expect, test, type Browser, type Page } from "@playwright/test";

import { seedWorkspace, type Person } from "./seed";

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

/** Adds a task from page's current form and answers its URL. */
async function addTask(page: Page, title: string, assignee?: string) {
  const form = page.getByRole("form", { name: "New task" });
  await form.getByRole("textbox", { name: "Task title" }).fill(title);
  if (assignee)
    await form
      .getByRole("combobox", { name: "Assignee" })
      .selectOption({ label: assignee });
  await form.getByRole("button", { name: "Add task" }).click();
  await expect(page).toHaveURL(/\/tasks\/[a-z2-7]{12}$/);
  return page.url();
}

test.describe("tasks", () => {
  test("a task assigned to bob moves across the board, and its comment is kept", async ({
    browser,
  }) => {
    const ws = await seedWorkspace("ada", "bob");
    const [ada, bob] = await Promise.all(
      ws.members.map((m) => signedIn(browser, m)),
    );
    await ada.goto(`/${ws.orgSlug}/tasks`);
    const url = await addTask(ada, "Write the launch post", "bob");

    await bob.goto(`/${ws.orgSlug}/tasks?view=board`);
    const todo = bob.getByRole("region", { name: "To do" });
    await expect(todo).toContainText("Write the launch post");
    await todo
      .getByRole("button", { name: "Move Write the launch post to Doing" })
      .click();
    await expect(bob.getByRole("region", { name: "Doing" })).toContainText(
      "Write the launch post",
    );
    await expect(todo).not.toContainText("Write the launch post");

    await bob.goto(url);
    await bob
      .getByRole("form", { name: "Comment" })
      .getByRole("textbox", { name: "Comment" })
      .fill("draft is up");
    await bob.getByRole("button", { name: "Comment" }).click();
    await expect(bob.getByRole("region", { name: "Comments" })).toContainText(
      "draft is up",
    );

    await ada.goto(`/${ws.orgSlug}/tasks`);
    const listed = ada.getByRole("list", { name: "Tasks" });
    await expect(listed).toContainText("Doing");
    await expect(listed).toContainText("bob");
    await ada.goto(url);
    await expect(ada.getByRole("region", { name: "Comments" })).toContainText(
      "draft is up",
    );
  });

  test("a task made from a page and one from a message link them both ways", async ({
    browser,
  }) => {
    const ws = await seedWorkspace("ada");
    const ada = await signedIn(browser, ws.members[0]);

    await ada.goto(`/${ws.orgSlug}`);
    await ada.getByRole("textbox", { name: "New page title" }).fill("Plan");
    await ada.getByRole("button", { name: "Add" }).first().click();
    await expect(ada).toHaveURL(new RegExp(`/${ws.orgSlug}/[a-z2-7]{12}$`));
    const pageUrl = ada.url();
    await ada.getByRole("link", { name: "Add a task for this page" }).click();
    await addTask(ada, "Review the plan");
    await expect(ada.getByRole("region", { name: "Links" })).toContainText(
      "Plan",
    );
    await ada.goto(pageUrl);
    await expect(
      ada.getByRole("region", { name: "Linked tasks" }),
    ).toContainText("Review the plan");

    await ada.goto(`/${ws.orgSlug}/channels`);
    await ada.getByRole("textbox", { name: "Channel name" }).fill("general");
    await ada.getByRole("button", { name: "Create" }).click();
    await expect(ada.getByTestId("live-status").first()).toHaveText("live");
    await ada.getByRole("textbox", { name: "Message" }).fill("ship friday");
    await ada.getByRole("button", { name: "Send" }).click();
    const message = ada
      .getByTestId("message")
      .filter({ hasText: "ship friday" });
    const linked = message.getByRole("link", { name: "Tasks" });
    const linkedUrl = await linked.getAttribute("href");
    await linked.click();
    await addTask(ada, "Prepare the release");
    await expect(ada.getByRole("region", { name: "Links" })).toContainText(
      "ship friday",
    );
    await ada.goto(linkedUrl!);
    const tasks = ada.getByRole("list", { name: "Tasks" });
    await expect(tasks).toContainText("Prepare the release");
    await expect(tasks).not.toContainText("Review the plan");
  });
});
