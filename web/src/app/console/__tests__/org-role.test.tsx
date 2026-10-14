import { act, render, screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { OrgRoleRead } from "@/app/console/_components/org-role-read";
import { ROLE_LABEL, useOrgRole } from "@/app/console/_components/org-role";
import { API_URL, aliceMembership } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

const route = vi.hoisted(() => ({ pathname: "/console/orgs/acme" }));

// Thrown in the shape Next.js gives each, digest included, which is
// what tells a redirect apart from a not-found.
vi.mock("next/navigation", () => ({
  usePathname: () => route.pathname,
  notFound: () => {
    throw Object.assign(new Error("NEXT_HTTP_ERROR_FALLBACK;404"), {
      digest: "NEXT_HTTP_ERROR_FALLBACK;404",
    });
  },
  redirect: (url: string) => {
    throw Object.assign(new Error("NEXT_REDIRECT"), {
      digest: `NEXT_REDIRECT;replace;${url};307;`,
    });
  },
}));

function RoleProbe() {
  const role = useOrgRole();
  return <p>{role ? ROLE_LABEL[role] : "No role"}</p>;
}

async function renderRead(pathname = "/console/orgs/acme") {
  route.pathname = pathname;
  const report = await OrgRoleRead({ org: Promise.resolve("acme") });
  await act(async () => {
    render(
      <>
        {report}
        <RoleProbe />
      </>,
    );
  });
}

describe("the caller's role in an organization", () => {
  it("is read from the members list and handed to the shell", async () => {
    await renderRead();

    expect(
      screen.getByText(ROLE_LABEL[aliceMembership.role]),
    ).toBeInTheDocument();
  });

  it("is not claimed for another organization's pages", async () => {
    // The report names acme; the shell is on globex.
    await renderRead("/console/orgs/globex");

    expect(screen.getByText("No role")).toBeInTheDocument();
  });

  it("is left out when the members list is refused", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/members`, () =>
        HttpResponse.json({ message: "forbidden" }, { status: 403 }),
      ),
    );

    await renderRead();

    expect(screen.getByText("No role")).toBeInTheDocument();
  });

  it("is left out when the API does not answer", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/members`, () => HttpResponse.error()),
    );

    await renderRead();

    expect(screen.getByText("No role")).toBeInTheDocument();
  });

  it("is left out when the caller is not among the members", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/members`, () =>
        HttpResponse.json([
          { ...aliceMembership, userGrn: "grn:root:default:user:bob" },
        ]),
      ),
    );

    await renderRead();

    expect(screen.getByText("No role")).toBeInTheDocument();
  });

  it("is left out, not a failed page, when the members read errors", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/members`, () =>
        HttpResponse.json({ message: "internal" }, { status: 500 }),
      ),
    );

    await renderRead();

    expect(screen.getByText("No role")).toBeInTheDocument();
  });

  it("is left out, not a not-found page, when the members read is 404", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/members`, () =>
        HttpResponse.json({ message: "not found" }, { status: 404 }),
      ),
    );

    await renderRead();

    expect(screen.getByText("No role")).toBeInTheDocument();
  });

  it("lets the redirect to sign-in through when the session has ended", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/members`, () =>
        HttpResponse.json({ message: "unauthenticated" }, { status: 401 }),
      ),
    );

    // Control for the two above: the one failure that is not the read's
    // own still reaches Next.js.
    await expect(
      OrgRoleRead({ org: Promise.resolve("acme") }),
    ).rejects.toMatchObject({
      digest: expect.stringMatching(/^NEXT_REDIRECT;/),
    });
  });
});
