# ADR 0152: garam may log in to OpenBao with a workload identity file, beside the token file

> Status: accepted
> Date: 2026-11-13

Drafted for #1830, which reserves this number. It adds a second form to
what [ADR-0104](./0104-message-signing-keys-may-be-held-by-an-openbao-transit-provider-and-the-file-key-encryption-key-stays-the-default.md)
§Decision decided of how garam holds the Transit token, and it reverses
one line of that ADR's §Alternatives considered: "Kubernetes auth
implemented in garam". It supersedes nothing: the token-file form stays
as ADR-0104 wrote it.

## Context

ADR-0104 had garam read the OpenBao token from a file on every call and
log in with nothing, leaving whatever writes and renews that file, and
the auth role it logs in under, to the deployment. It rejected login in
garam because that needed "login and lease renewal in garam, and a
second method for a standalone host; the token file serves both with
none". That held where something runs beside garam to write the file:
OpenBao Agent's auto-auth in a cluster.

A gaon MicroVM has no such process. gaon's decision D16, recorded on
`garamsh/gaon` issue 117 and carried by its issue 159, is that no second
process runs beside the workload, so the exchange at a secret store is
the application's own. What gaon decided to give the workload is a
workload identity token in a file, written at a fixed path and renewed
in place by atomic rename at 80% of its lifetime. A garam in such a
MicroVM that holds its message signing keys in Transit therefore has
nothing to write the OpenBao token file, and ADR-0104's form cannot be
used there.

What gaon serves and what it has only decided, read on 2026-11-13 at
`garamsh/gaon` commit `9941e390da42b49071ded0508f953ccac60e6e58` (its
`main`; its parent is `87d51e475acae9f27e61c269c46be6e2720704e7`,
where `docs/adr/0020-*`, `resource-api.md` and `node-agent.md` were
read):

- **Served.** `POST /serviceaccounts/{name}/token` signs a JWT under
  ES256 with `--service-account-signing-key-file`, claims `iss`
  (`--service-account-issuer`), `sub` (`gaon:serviceaccount:<name>`),
  `aud` (the audiences the request names), `iat`, `exp` and a
  `gaon.garam.sh` claim holding the service account and MicroVM
  (`apiserver.md`).
- **Not served.** The OIDC discovery document and the key set
  (`garamsh/gaon` issue 161, open), so an OpenBao cannot yet read
  gaon's keys from gaon. The Node Agent's writing and renewing of the
  token file (issue 162, open; `node-agent.md` has no section on it).
  The token file's path, owner and mode, which no gaon document states.
- **Decided, not built.** gaon's API server serves discovery and the key
  set without authentication under a stable `iss` on the control plane's
  address, verified by the cluster CA that a MicroVM can be given as a
  file (D16). Issue 162 says a refused renewal is retried and no token
  value appears in logs, events, status or errors.

OpenBao's JWT auth method is the other half
(<https://openbao.org/docs/api/auth/jwt/>, read 2026-11-13). A login is
`POST /v1/auth/<mount>/login` with `role` and `jwt`, and answers
`auth.client_token`, `auth.lease_duration` and `auth.renewable`. The
method's mount is configured with exactly one of `oidc_discovery_url`,
`jwks_url` or `jwt_validation_pubkeys`, and `bound_issuer`; a role
binds `bound_audiences` and sets the policies and lease of the token it
grants. A token renews itself with `POST /v1/auth/token/renew-self`
(<https://openbao.org/docs/api/auth/token/>, read 2026-11-13).

## Decision

**`GARAM_TRANSIT_IDENTITY_FILE` names a workload identity token garam
logs in with, and it is the second way to give garam the OpenBao
token.** Exactly one of it and `GARAM_TRANSIT_TOKEN_FILE` is set.

1. **The exchange is garam's.** garam posts the file's contents and the
   role to the JWT auth method's login, holds the OpenBao token it is
   given in memory alone, and presents it in the `X-Vault-Token` header
   as the token-file form does. No token is written to disk, put in an
   error or logged, and neither is the identity.
2. **Three settings.** `GARAM_TRANSIT_IDENTITY_FILE`;
   `GARAM_TRANSIT_AUTH_ROLE`, required with it; and
   `GARAM_TRANSIT_AUTH_MOUNT`, the method's mount, `jwt` when unset and
   a path of ordinary segments otherwise. The last two with
   `GARAM_TRANSIT_TOKEN_FILE` are refused, because that form logs in to
   nothing, and so is a start that sets both files or neither. The
   existing rule stands beside them: with `GARAM_TRANSIT_ADDR` unset,
   any `GARAM_TRANSIT_*` set is refused.
3. **The identity file is held to the key-file rule.** It is read
   through `platform/internal/keyfile`, so a file with a group or other
   permission bit, or owned by another UID, is refused
   ([ADR-0015](./0015-operator-identity.md)). It is read once at start,
   to refuse a start where it could never be read and keeping nothing
   of it, and again at every login, so an identity renewed in place is
   the one presented. A file not yet written when a call needs it fails
   that call as unavailable, as a token file not yet written does.
4. **Renewal and login again happen in the call, not beside it.** No
   goroutine is added. A call that finds no token logs in. A call made
   after two thirds of the lease the last login or renewal granted
   renews with `renew-self`. Where renewal is refused, or the token is
   not renewable, the call logs in again. Where OpenBao cannot answer
   the renewal, a token whose lease has not ended keeps serving, and one
   whose lease has ended is logged in for again.
5. **A revoked token costs one login.** A Transit call answered 403
   under a token that had already served an earlier call drops the
   token, logs in again and is sent once more. A 403 to a token just
   obtained is the policy's and is returned, so a policy that denies
   the key is not retried.
6. **The token-file form is unchanged.** It reads its file on every
   call as before, and its tests pass without edit.

**Where gaon's file cannot meet the owner-only rule, the rule is not
relaxed here.** gaon's contract states no owner or mode for the token
file, and its MicroVM `files` entry defaults to mode 0644 and the
MicroVM's user. This ADR holds the identity file to the rule where
gaon's file allows it, and records that nothing yet says whether it
does. A knob that accepts a group-readable identity file would be a
later decision, taken against gaon's stated owner and mode.

**What the deployment owes, which garam does not create.** An OpenBao
JWT auth method whose configuration names gaon's issuer and a way to
read its keys; a role binding `bound_audiences` to the audience the
service account token request names and granting the signing policy of
ADR-0104; and, for renewal, a role whose tokens may call
`auth/token/renew-self`. Read against OpenBao 2.7.2 on 2026-11-13, a
role that sets `token_no_default_policy` and omits that path has every
`renew-self` answered 403, and garam then logs in again at each lease's
end instead of renewing: it works, at a login per lease.

## Consequences

- **A sidecar-less deployment can hold its message signing keys in
  Transit.** Nothing writes a token file for it.
- **garam holds a credential in memory that it obtained itself.** The
  OpenBao token is in garam's memory for its lease, as the token-file
  form's is for a call, and the identity file's contents are held for
  the length of a login and no longer. The token's reach is ADR-0104's
  two paths.
- **A start with an unreadable or group-readable identity file does not
  happen.** That includes an identity file not yet written, where a
  token file not yet written is waited for, because the identity file
  is read at start. Whether gaon writes the file before it starts the
  workload is not stated in any gaon document read; a MicroVM that
  starts garam ahead of the file is refused at start.
- **The identity's issuer is read by OpenBao, not by garam.** Until gaon
  serves discovery and the key set, a deployment gives the OpenBao
  mount `jwt_validation_pubkeys` or reads the keys by another route; the
  integration tests do the former with a key they generate, because
  gaon serves neither.
- **Token lifetime is the role's.** The call that renews does the work
  and no timer fires, so a garam that makes no Transit call for a whole
  lease holds an expired token and its next call logs in again.
- **`deploy/charts/garam/README.md` §Not set by this chart lists the
  three variables.** The chart renders the token-file form alone,
  because a cluster has a process to write the file.
- **`docs/architecture/deployment-contract.md` states both forms.** Its
  account of the gaon side is a pointer to what gaon has decided: the
  gaon deployment contract does not exist yet and is not written here.

## Signs the decision was wrong

- gaon states a token-file owner or mode that cannot be owner-only for
  the process garam runs as. The key-file rule would then refuse the
  file gaon writes, and the decision to hold the identity to it, or
  gaon's file, is what changes.
- A second sidecar-less deployment appears whose identity is not a JWT
  OpenBao's JWT method can verify, which would make this a second form
  for one case rather than a form a class of deployments uses.
- A deployment reports a start refused for an identity file not yet
  written, which would mean the read at start costs more than the
  refusal it buys, and the start read is what is dropped.
- Logins at OpenBao that outnumber leases by a wide margin in a
  deployment whose role does grant `renew-self`, which would mean
  renewal is failing for a reason the call's fallback hides.
