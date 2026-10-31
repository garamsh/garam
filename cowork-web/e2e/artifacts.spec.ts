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

/**
 * An artifact that tries everything a hostile page would: read the
 * viewer's cookies and storage, call cowork's API as the viewer, and
 * reach into the page that frames it. It writes what each attempt got,
 * and that it ran at all.
 */
const probe = (org: string) => `<!doctype html>
<body><pre id="out">running</pre>
<script>
  const out = [];
  const record = (name, f) => {
    try { out.push(name + "=" + f()); } catch (e) { out.push(name + "=blocked"); }
  };
  record("cookie", () => document.cookie || "empty");
  record("storage", () => { localStorage.setItem("k", "v"); return "open"; });
  record("parent", () => window.parent.document.title);
  document.getElementById("out").textContent = out.join("\\n");
  fetch("http://localhost:8091/orgs/${org}/pages", { credentials: "include" })
    .then((r) => r.text().then((t) => "fetch=" + r.status + " " + t))
    .catch(() => "fetch=blocked")
    .then((line) => { document.getElementById("out").textContent += "\\n" + line + "\\ndone"; });
</script></body>`;

/** Creates an HTML artifact from the artifacts page and answers its URL. */
async function createHTML(
  page: Page,
  org: string,
  title: string,
  html: string,
) {
  await page.goto(`/${org}/artifacts`);
  const form = page.getByRole("form", { name: "New artifact" });
  await form.getByRole("textbox", { name: "Artifact title" }).fill(title);
  await form.getByRole("textbox", { name: "HTML" }).fill(html);
  await form.getByRole("button", { name: "Create" }).click();
  await expect(page).toHaveURL(new RegExp(`/${org}/artifacts/[a-z2-7]{12}$`));
  return page.url();
}

test.describe("artifacts", () => {
  test("a script in an artifact runs, and reaches neither the viewer's session nor cowork's API", async ({
    browser,
  }) => {
    const ws = await seedWorkspace("ada");
    const ada = await signedIn(browser, ws.members[0]);
    const pageTitle = "Secret page title";
    await ada.goto(`/${ws.orgSlug}`);
    await ada.getByRole("textbox", { name: "New page title" }).fill(pageTitle);
    await ada.getByRole("button", { name: "Add" }).first().click();
    await expect(ada).toHaveURL(new RegExp(`/${ws.orgSlug}/[a-z2-7]{12}$`));

    await createHTML(ada, ws.orgSlug, "Probe", probe(ws.orgSlug));
    const frame = ada.locator('iframe[title="Probe"]');
    await expect(frame).toHaveAttribute("sandbox", "allow-scripts");
    await expect(frame).toHaveAttribute("src", /^http:\/\/localhost:8093\/a\//);

    const out = frame.contentFrame().locator("#out");
    await expect(out).toContainText("done");
    const text = (await out.textContent()) ?? "";
    expect(text).toContain("cookie=blocked");
    expect(text).toContain("storage=blocked");
    expect(text).toContain("parent=blocked");
    expect(text).toContain("fetch=blocked");
    expect(text).not.toContain(pageTitle);

    // The control: the same session reads the page tree on cowork's
    // own origin, so what the artifact was refused is there to refuse.
    const tree = await ada.request.get(`/${ws.orgSlug}`);
    expect(await tree.text()).toContain(pageTitle);
  });

  test("every save is a version, and each stays readable", async ({
    browser,
  }) => {
    const ws = await seedWorkspace("ada");
    const ada = await signedIn(browser, ws.members[0]);
    const url = await createHTML(ada, ws.orgSlug, "Chart", "<h1>first</h1>");
    await ada
      .getByRole("form", { name: "New version" })
      .getByRole("textbox", { name: "HTML" })
      .fill("<h1>second</h1>");
    await ada.getByRole("button", { name: "Save a new version" }).click();

    const frame = () =>
      ada.locator('iframe[title="Chart"]').contentFrame().locator("h1");
    await expect(frame()).toHaveText("second");
    await ada
      .getByRole("navigation", { name: "Versions" })
      .getByRole("link", { name: "v1" })
      .click();
    await expect(frame()).toHaveText("first");
    await expect(ada).toHaveURL(`${url}?version=1`);
  });

  test("a page and a message embed an artifact", async ({ browser }) => {
    const ws = await seedWorkspace("ada", "bob");
    const [ada, bob] = await Promise.all(
      ws.members.map((m) => signedIn(browser, m)),
    );
    const url = await createHTML(
      ada,
      ws.orgSlug,
      "Diagram",
      "<h1>diagram</h1>",
    );
    const id = url.split("/").pop()!;

    await ada.goto(`/${ws.orgSlug}`);
    await ada.getByRole("textbox", { name: "New page title" }).fill("Notes");
    await ada.getByRole("button", { name: "Add" }).first().click();
    await expect(ada.getByTestId("sync-status")).toHaveText("connected");
    ada.once("dialog", (d) => void d.accept(id));
    await ada.getByRole("button", { name: "Artifact" }).click();
    const notes = ada.url();
    await bob.goto(notes);
    await expect(
      bob.locator('iframe[title="Diagram"]').contentFrame().locator("h1"),
    ).toHaveText("diagram");

    await ada.goto(`/${ws.orgSlug}/channels`);
    await ada.getByRole("textbox", { name: "Channel name" }).fill("general");
    await ada.getByRole("button", { name: "Create" }).click();
    await expect(ada.getByTestId("live-status").first()).toHaveText("live");
    await ada.getByRole("textbox", { name: "Message" }).fill("see this");
    await ada
      .getByRole("combobox", { name: "Embed an artifact" })
      .selectOption({ label: "Diagram" });
    await ada.getByRole("button", { name: "Send" }).click();
    await bob.goto(ada.url());
    const message = bob.getByTestId("message").filter({ hasText: "see this" });
    await expect(
      message.locator('iframe[title="Diagram"]').contentFrame().locator("h1"),
    ).toHaveText("diagram");
  });
});
