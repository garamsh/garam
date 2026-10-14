import { describe, expect, it } from "vitest";

import { secretSnippet } from "@/app/console/orgs/[org]/operators/[operator]/connect/_components/secret-snippet";

const placement = {
  keyFile: "./field-one.pem",
  clientId: "grn:acme:default:operator:field-one",
  tokenUrl: "https://machine.garam.example/oauth/token",
};

describe("the Secret a key is placed in", () => {
  it("places the manager's key in the Secret its base mounts, under the keys it reads", () => {
    expect(secretSnippet("manager", placement)).toBe(
      [
        "kubectl create secret generic garam-credential \\",
        "  --namespace garam-agent-operator-system \\",
        "  --from-file=private-key.pem=./field-one.pem \\",
        "  --from-literal=client-id='grn:acme:default:operator:field-one' \\",
        "  --from-literal=token-url='https://machine.garam.example/oauth/token' \\",
        "  --from-file=server-root.pem=./garam-server-root.pem \\",
        "  --dry-run=client -o yaml | kubectl apply --server-side --force-conflicts -f -",
      ].join("\n"),
    );
  });

  it("leaves the control service's Secret name and namespace as placeholders", () => {
    const snippet = secretSnippet("control", placement);

    expect(snippet).toContain("<control-service-secret>");
    expect(snippet).toContain("--namespace <control-service-namespace>");
    // Its Secret holds no server root: the control service takes that
    // by a flag of its own.
    expect(snippet).not.toContain("server-root.pem");
  });

  it("takes the private key only from its file, never on the command line", () => {
    for (const identity of ["manager", "control"] as const) {
      const snippet = secretSnippet(identity, placement);
      expect(snippet).toContain("--from-file=private-key.pem=./field-one.pem");
      expect(snippet).not.toMatch(/--from-literal=private-key/);
      expect(snippet).not.toContain("PRIVATE KEY");
    }
  });

  it("applies server-side, so no annotation keeps a copy of the private key", () => {
    for (const identity of ["manager", "control"] as const) {
      const snippet = secretSnippet(identity, placement);
      // A client-side apply writes the whole object, key included, into
      // `last-applied-configuration`; server-side apply writes none.
      expect(snippet).toContain(
        "kubectl apply --server-side --force-conflicts -f -",
      );
      expect(snippet).not.toMatch(/kubectl apply -f -/);
      expect(snippet).not.toMatch(/kubectl apply(?! --server-side)/);
    }
  });
});
