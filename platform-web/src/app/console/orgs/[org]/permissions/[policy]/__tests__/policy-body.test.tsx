import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import {
  PolicyBody,
  bodyText,
} from "@/app/console/orgs/[org]/permissions/[policy]/_components/policy-body";
import type { IamPolicyBody } from "@/features/orgs";

const roleLink: Extract<IamPolicyBody, { kind: "role-link" }> = {
  kind: "role-link",
  roleLink: {
    role: "owner",
    scope: "organization",
    principal: "grn:acme/iam/group/eng",
    resource: "grn:root/iam/org/acme",
  },
};

describe("bodyText", () => {
  it("gives Cedar text as written, for a guardrail and a permit alike", () => {
    expect(bodyText({ kind: "guardrail", text: "forbid (a);" })).toBe(
      "forbid (a);",
    );
    expect(bodyText({ kind: "permit", text: "permit (a);" })).toBe(
      "permit (a);",
    );
  });

  it("gives a role link as its four facts, one to a line", () => {
    expect(bodyText(roleLink)).toBe(
      [
        "role: owner",
        "scope: organization",
        "principal: grn:acme/iam/group/eng",
        "resource: grn:root/iam/org/acme",
      ].join("\n"),
    );
  });
});

describe("PolicyBody", () => {
  it("shows a role link's role, scope, principal and resource", () => {
    render(<PolicyBody body={roleLink} />);

    expect(screen.getByText("owner")).toBeInTheDocument();
    expect(screen.getByText("Organization")).toBeInTheDocument();
    expect(screen.getByText("grn:acme/iam/group/eng")).toBeInTheDocument();
    expect(screen.getByText("grn:root/iam/org/acme")).toBeInTheDocument();
  });

  it("names the scope a role link reaches by its own word", () => {
    render(
      <PolicyBody
        body={{
          kind: "role-link",
          roleLink: {
            ...roleLink.roleLink,
            scope: "team-or-project",
          },
        }}
      />,
    );

    expect(screen.getByText("Team or project")).toBeInTheDocument();
    expect(screen.queryByText("Organization")).not.toBeInTheDocument();
  });

  it("shows Cedar text read-only, in a region a keyboard can scroll", () => {
    render(<PolicyBody body={{ kind: "permit", text: "permit (a);" }} />);

    const text = screen.getByLabelText("Cedar text");
    expect(text).toHaveTextContent("permit (a);");
    expect(text).toHaveAttribute("tabindex", "0");
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
  });
});
