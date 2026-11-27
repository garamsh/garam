import { render, screen, within } from "@testing-library/react";
import { Component, type ReactNode } from "react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { InstallDialog } from "@/app/console/orgs/[org]/apps/_components/install-dialog";
import {
  API_URL,
  botConnector,
  botConnectorInstallation,
  deployHelper,
} from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

// The real `unstable_rethrow` tells a redirect from a failed request,
// so the mocked one carries the digest a real redirect does.
vi.mock("next/navigation", async (importOriginal) => ({
  ...(await importOriginal<typeof import("next/navigation")>()),
  redirect: (url: string) => {
    throw Object.assign(new Error(`NEXT_REDIRECT:${url}`), {
      digest: `NEXT_REDIRECT;push;${url};307;`,
    });
  },
}));
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

/**
 * Shows what the form's action threw. An install that succeeds ends in
 * `redirect()`, which throws to navigate; this is where it lands.
 */
class Navigation extends Component<
  { children: ReactNode },
  { to: string | null }
> {
  state = { to: null };
  static getDerivedStateFromError(error: Error) {
    return { to: error.message };
  }
  render() {
    return this.state.to ?? this.props.children;
  }
}

const PROJECTS = [
  { grn: "grn:acme/iam/project/frontend", name: "Frontend" },
  { grn: "grn:acme/iam/project/backend", name: "Backend" },
];

/** Records the bodies the dialog installs with, and answers as garam does. */
function recordInstalls() {
  const bodies: unknown[] = [];
  server.use(
    http.post(`${API_URL}/orgs/acme/installations`, async ({ request }) => {
      bodies.push(await request.json());
      return HttpResponse.json(botConnectorInstallation, { status: 201 });
    }),
  );
  return bodies;
}

async function open(app = deployHelper) {
  const user = userEvent.setup();
  render(
    <Navigation>
      <InstallDialog org="acme" app={app} projects={PROJECTS} />
    </Navigation>,
  );
  await user.click(
    await screen.findByRole("button", { name: `Install ${app.name}` }),
  );
  return { user, dialog: within(screen.getByRole("dialog")) };
}

describe("install dialog", () => {
  it("shows the requested actions and the scope before anything is sent", async () => {
    const installs = recordInstalls();
    const { dialog } = await open();

    expect(
      dialog.getByRole("heading", { name: "Install Deploy helper" }),
    ).toBeInTheDocument();
    expect(dialog.getByText("iam:project.read")).toBeInTheDocument();
    expect(dialog.getByText("iam:agent.read")).toBeInTheDocument();
    expect(
      dialog.getByRole("radio", { name: "The whole organization" }),
    ).toBeChecked();
    expect(
      dialog.getByRole("radio", { name: "Chosen projects" }),
    ).not.toBeChecked();
    expect(installs).toEqual([]);
  });

  it("says a bot-hosting request is not granted by installing", async () => {
    const { dialog } = await open();

    expect(
      dialog.getByText(/Installing does not grant that/),
    ).toBeInTheDocument();
  });

  it("says an app that requests nothing writes no permission", async () => {
    const { dialog } = await open(botConnector);

    expect(
      dialog.getByText(
        "It requests no actions, so installing writes no permission.",
      ),
    ).toBeInTheDocument();
    expect(
      dialog.queryByText(/Installing does not grant/),
    ).not.toBeInTheDocument();
  });

  it("installs over the whole organization and opens the installation", async () => {
    const installs = recordInstalls();
    const { user, dialog } = await open();

    await user.click(
      dialog.getByRole("button", { name: "Install Deploy helper" }),
    );

    expect(
      await screen.findByText(
        "NEXT_REDIRECT:/console/orgs/acme/apps/deploy-helper",
      ),
    ).toBeInTheDocument();
    expect(installs).toEqual([{ app: "deploy-helper" }]);
  });

  it("installs over the projects chosen, and only once one is chosen", async () => {
    const installs = recordInstalls();
    const { user, dialog } = await open();

    await user.click(dialog.getByRole("radio", { name: "Chosen projects" }));
    const confirm = dialog.getByRole("button", {
      name: "Install Deploy helper",
    });
    expect(confirm).toBeDisabled();
    await user.click(dialog.getByRole("checkbox", { name: "Backend" }));
    expect(confirm).toBeEnabled();
    await user.click(confirm);

    await screen.findByText(/NEXT_REDIRECT/);
    expect(installs).toEqual([
      { app: "deploy-helper", projects: ["grn:acme/iam/project/backend"] },
    ]);
  });

  it("keeps garam's refusal in the dialog", async () => {
    server.use(
      http.post(`${API_URL}/orgs/acme/installations`, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "only an owner installs" },
          { status: 403 },
        ),
      ),
    );
    const { user, dialog } = await open();

    await user.click(
      dialog.getByRole("button", { name: "Install Deploy helper" }),
    );

    expect(
      await dialog.findByText(/only an owner installs|owner/),
    ).toBeInTheDocument();
    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });

  it("sends nothing when cancelled", async () => {
    const installs = recordInstalls();
    const { user, dialog } = await open();

    await user.click(dialog.getByRole("button", { name: "Cancel" }));

    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(installs).toEqual([]);
  });
});
