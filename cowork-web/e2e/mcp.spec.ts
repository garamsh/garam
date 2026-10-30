import { expect, test, type Browser, type Page } from "@playwright/test";

import { seedWorkspace, type Person } from "./seed";

/** The cowork binary the suite serves, which takes the MCP calls. */
const COWORK_URL = "http://localhost:8091";

function apiUrl(): string {
  const url = process.env.GARAM_API_URL;
  if (!url)
    throw new Error(
      "GARAM_API_URL is unset — run the suite through `make cowork-web-e2e`",
    );
  return url;
}

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
 * Signs a CLI in as person by the platform's device grant, as `cowork
 * login` does, with person approving it as the console's page would,
 * and answers the CLI session's bearer.
 */
async function cliBearer(person: Person): Promise<string> {
  const form = (values: Record<string, string>) => ({
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body: new URLSearchParams(values).toString(),
  });
  const started = await fetch(
    `${apiUrl()}/auth/device`,
    form({ client_id: "garam-cli", client_name: "cowork CLI on e2e" }),
  );
  expect(started.status).toBe(200);
  const code = (await started.json()) as {
    device_code: string;
    user_code: string;
    interval: number;
  };
  const approved = await fetch(`${apiUrl()}/auth/device/approval`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Cookie: `garam_session=${person.sessionToken}`,
    },
    body: JSON.stringify({ userCode: code.user_code, approve: true }),
  });
  expect(approved.status).toBe(204);
  for (;;) {
    await new Promise((r) => setTimeout(r, code.interval * 1000));
    const polled = await fetch(
      `${apiUrl()}/auth/device/token`,
      form({
        client_id: "garam-cli",
        grant_type: "urn:ietf:params:oauth:grant-type:device_code",
        device_code: code.device_code,
      }),
    );
    const body = (await polled.json()) as {
      access_token?: string;
      error?: string;
    };
    if (body.access_token) return body.access_token;
    expect(body.error).toBe("authorization_pending");
  }
}

type ToolResult = {
  isError?: boolean;
  structuredContent?: Record<string, unknown>;
  content: { type: string; text?: string }[];
};

/** Calls one of cowork's MCP tools as bearer's caller. */
async function callTool(
  bearer: string,
  name: string,
  args: Record<string, unknown>,
): Promise<ToolResult> {
  const resp = await fetch(`${COWORK_URL}/mcp`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Accept: "application/json, text/event-stream",
      Authorization: `Bearer ${bearer}`,
    },
    body: JSON.stringify({
      jsonrpc: "2.0",
      id: 1,
      method: "tools/call",
      params: { name, arguments: args },
    }),
  });
  expect(resp.status).toBe(200);
  const reply = (await resp.json()) as { result: ToolResult };
  return reply.result;
}

const editorOf = (page: Page) =>
  page.getByRole("textbox", { name: "Page content" });

test.describe("mcp", () => {
  test("an agent's edit appears in an open page as it is made", async ({
    browser,
  }) => {
    test.slow(); // the device grant polls every five seconds
    const ws = await seedWorkspace("ada");
    const [adaPerson] = ws.members;
    const ada = await signedIn(browser, adaPerson);

    await ada.goto(`/${ws.orgSlug}`);
    await ada.getByRole("textbox", { name: "New page title" }).fill("Notes");
    await ada.getByRole("button", { name: "Add" }).first().click();
    await expect(ada).toHaveURL(new RegExp(`/${ws.orgSlug}/[a-z2-7]{12}$`));
    const pageId = new URL(ada.url()).pathname.split("/").pop()!;
    await expect(ada.getByTestId("sync-status")).toHaveText("connected");
    await editorOf(ada).click();
    await ada.keyboard.type("typed in the browser");

    const bearer = await cliBearer(adaPerson);
    await expect
      .poll(async () => {
        const read = await callTool(bearer, "read_page", {
          org: ws.orgSlug,
          page: pageId,
        });
        return read.structuredContent?.markdown;
      })
      .toBe("typed in the browser\n");

    const updated = await callTool(bearer, "update_page", {
      org: ws.orgSlug,
      page: pageId,
      edits: [
        {
          kind: "append",
          block: {
            type: "heading",
            attrs: { level: 2 },
            text: "From the agent",
          },
        },
      ],
    });
    expect(updated.isError ?? false).toBe(false);
    await expect(editorOf(ada).locator("h2")).toHaveText("From the agent");
    await expect(editorOf(ada).locator("p").first()).toHaveText(
      "typed in the browser",
    );
  });

  test("a caller outside the workspace is answered an error, not the page", async () => {
    test.slow();
    const ws = await seedWorkspace("ada");
    const outsider = await cliBearer(ws.outsider);
    const workspaces = await callTool(outsider, "list_workspaces", {});
    expect(workspaces.structuredContent?.workspaces).toEqual([]);
    const listed = await callTool(outsider, "list_pages", { org: ws.orgSlug });
    expect(listed.isError).toBe(true);
    expect(listed.structuredContent).toBeUndefined();
  });
});
