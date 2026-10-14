/** Which of an Operator's two identities a key is placed for. */
export type Identity = "manager" | "control";

/** What a snippet places: the key's file and the two values beside it. */
export type Placement = {
  /** The file the private key was saved to. */
  keyFile: string;
  clientId: string;
  tokenUrl: string;
};

/**
 * The Secret each identity reads its key from, as garam-agent-operator
 * `c730884` has it. The manager's base mounts `garam-credential`
 * read-only in `garam-agent-operator-system` and reads the keys
 * `private-key.pem`, `client-id`, `token-url` and `server-root.pem`
 * (`config/manager/manager.yaml`, `docs/architecture/configuration.md`).
 * The control service ships no manifest there, so its Secret's name and
 * namespace are the deployment's own, written as placeholders.
 */
const SECRET: Record<Identity, { name: string; namespace: string }> = {
  manager: {
    name: "garam-credential",
    namespace: "garam-agent-operator-system",
  },
  control: {
    name: "<control-service-secret>",
    namespace: "<control-service-namespace>",
  },
};

/**
 * The command that places a key in its identity's Secret. The private
 * key goes in only as a file, never on the command line. Applied
 * server-side, which replaces a Secret already there, as a rotation
 * does, and writes no `last-applied-configuration` annotation: a
 * client-side `apply` copies the whole object, private key included,
 * into one that `kubectl describe` prints. The manager's
 * Secret also holds garam's server root, which it verifies garam's
 * listener against, so replacing it carries that file too.
 */
export function secretSnippet(
  identity: Identity,
  placement: Placement,
): string {
  const secret = SECRET[identity];
  return [
    `kubectl create secret generic ${secret.name} \\`,
    `  --namespace ${secret.namespace} \\`,
    `  --from-file=private-key.pem=${placement.keyFile} \\`,
    `  --from-literal=client-id='${placement.clientId}' \\`,
    `  --from-literal=token-url='${placement.tokenUrl}' \\`,
    ...(identity === "manager"
      ? [`  --from-file=server-root.pem=./garam-server-root.pem \\`]
      : []),
    `  --dry-run=client -o yaml | kubectl apply --server-side --force-conflicts -f -`,
  ].join("\n");
}
