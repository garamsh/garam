# Runbook for installing, upgrading and recovering garam on a cluster

How to install the chart in this directory on a Kubernetes cluster, move
a release to the next, and recover it. What the cluster must provide and
how the Secrets are produced is `README.md` §What a cluster must
provide and §Producing the Secrets for a cluster; this file assumes
both are done. Names are the agreed ones: namespace and release
`garam`, so the workloads are `garam-api`, `garam-machine`,
`garam-policy`, `garam-web` and `garam-site`, and the migration Jobs
`garam-migrate` and `garam-policy-migrate`. Each step names the command
and what its output must show. No step prints a Secret's value: the
commands read names, counts and statuses.

## Contents
- Install
- Upgrade
- Recover

## Install

1. **Check out the release's tag.** `git checkout v0.1.0-dev.N` in a
   clone of `garamsh/garam`. The chart is read from
   `deploy/charts/garam` at that tag, and the image tag is the tag with
   the `v` dropped (README §Pinning a version). `git describe --tags`
   must print the tag.
2. **Produce the key material once.**
   `sh deploy/charts/cluster/key-material.sh <new-directory>` prints one
   line per file, each with its mode and fingerprint, and ends with
   `10 files written`. Store the directory and the printed lines in the
   secret store, then fill the Secrets in README §Producing the
   Secrets for a cluster from it. Skip this step on a cluster that has
   a key-encryption key: a second run makes a different one, and every
   message signing key sealed under the first opens under no other.
3. **Check the Secrets exist.**
   `kubectl -n garam get secret garam-db garam-policy-db ecr-pull garam-machine-key-material garam-policy-tls garam-policy-ca garam-policy-jwks garam-policy-signing-key`
   lists all eight with `AGE` and no `NotFound`. The key names inside
   are read, not printed:
   `kubectl -n garam get secret garam-machine-key-material -o go-template='{{range $k, $v := .data}}{{$k}} {{end}}'`
   prints `ca-kek.hex server-root.pem tls.crt tls.key`.
4. **Render first.** `helm template garam deploy/charts/garam -n garam
   -f <cluster-values.yaml> >/dev/null` exits 0. A refusal names the
   value to set and the reason.
5. **Install.** `helm upgrade --install garam deploy/charts/garam -n
   garam -f <cluster-values.yaml> --wait --timeout 10m`. The two
   migration Jobs run first, as `pre-install` hooks, one per database,
   and neither waits on the other (README §What it renders). The output
   must end `STATUS: deployed`.
6. **Check what ran.**
   - `kubectl -n garam get job` shows `garam-migrate` and
     `garam-policy-migrate` at `1/1` completions.
   - `kubectl -n garam get deploy` shows every Deployment `READY`
     equal to `UP-TO-DATE`.
   - `kubectl -n garam logs job/garam-migrate` ends without an error
     line.
   - `helm -n garam status garam` prints `STATUS: deployed`.
7. **Check the listeners.** The console answers over https on its host,
   and `openssl s_client -connect machine.garam.garam.sh:<port>
   -verify_return_error -CAfile <server-root.pem>` from a machine
   holding the stored `server-root.pem` ends `Verification: OK`.

## Upgrade

Between two releases, `v0.1.0-dev.N` to `v0.1.0-dev.M`:

1. **Read what changed.** `git diff v0.1.0-dev.N v0.1.0-dev.M --
   deploy/charts platform/migrations policy/migrations` names the
   chart and migration changes. A change to the Secrets in README
   §Producing the Secrets for a cluster is applied before the upgrade,
   not during it.
2. **Check the release is healthy first.** `helm -n garam status
   garam` prints `STATUS: deployed` and `helm -n garam history garam`
   names the revision to roll back to.
3. **One step, only from before chart 0.18.0.** An upgrade from a
   release whose policy Service is `ClusterIP` first runs
   `kubectl -n garam delete svc garam-policy`: `spec.clusterIP` is
   immutable, so `helm upgrade` fails on the headless Service
   otherwise. It is run by hand and not as a hook. A first install and
   an upgrade between releases that both render the headless Service
   need none.
4. **Check out the new tag, render, upgrade.** `git checkout
   v0.1.0-dev.M`; `helm template` as in Install step 4; then
   `helm upgrade garam deploy/charts/garam -n garam -f
   <cluster-values.yaml> --wait --timeout 10m`. Migrations run first:
   both Jobs are `pre-upgrade` hooks, so the new images do not start
   until each has completed. `kubectl -n garam get job` shows both at
   `1/1`.
5. **Check the result.** `helm -n garam status garam` prints
   `STATUS: deployed`, and `kubectl -n garam get deploy` shows every
   Deployment `READY` equal to `UP-TO-DATE`. Each Deployment is
   `RollingUpdate` with `maxUnavailable: 0` and `maxSurge: 1`
   (`templates/*.yaml`), so a new pod is ready before an old one goes
   and `kubectl -n garam rollout status deploy/garam-api` prints
   `successfully rolled out`.
6. **A failed migration.** The upgrade stops before any Deployment
   changes: the running release still serves, on the old images against
   the old schema. `kubectl -n garam logs job/garam-migrate` holds the
   only copy of the real error, and the Job is kept (its delete policy
   is `before-hook-creation`), so read it before another upgrade
   deletes it. The database is then dirty and every later run refuses
   until it is fixed by hand; the recovery is in
   `docs/architecture/deployment-contract.md` §The migrate job. Do not
   `helm rollback` to clear it: a rollback does not undo applied
   migrations.

## Recover

Each case starts from `kubectl -n garam get pods` and ends at the check
named.

- **A lost Pod.** `kubectl -n garam delete pod <pod>` replaces it, as
  the Deployment owns it. Within the Deployment's readiness probes
  (README §The policy service for the policy service's) the new Pod is
  `1/1 Ready`; `kubectl -n garam rollout status deploy/<name>` prints
  `successfully rolled out`. The other replica serves meanwhile.
- **A database restored from a backup.** Restore it with the database
  provider's own procedure, then:
  1. `kubectl -n garam scale deploy garam-api garam-machine
     --replicas=0` (and `garam-policy` if the policy database is the
     one restored), so nothing writes while the schema is wrong. This
     is a gap in service on purpose.
  2. `helm upgrade garam deploy/charts/garam -n garam -f
     <cluster-values.yaml> --wait`. The migration Jobs run again and
     apply whatever the backup predates; `kubectl -n garam logs
     job/garam-migrate` reports no pending or an applied-to-latest
     result and no error.
  3. The `helm upgrade` restores `replicas` to the chart's value, 2,
     so no scale-up is run. `kubectl -n garam rollout status
     deploy/garam-api` prints `successfully rolled out`, and the check
     is that sign-in answers and a policy decision is made.
- **The key material restored from the secret store.** The store holds
  the directory `key-material.sh` wrote and the lines it printed
  (Install step 2).
  1. Fill the Secrets from the stored files without regenerating
     anything. The key-encryption key is restored byte for byte: its
     first 16 SHA-256 hex digits, `openssl dgst -sha256
     garam-machine-key-material/ca-kek.hex | cut -c1-16` after the
     `= `, equal the `kek-sha256-16` the run printed.
  2. `kubectl -n garam rollout restart deploy/garam-machine
     deploy/garam-api deploy/garam-policy`: the pods read the files at
     start.
  3. `kubectl -n garam logs deploy/garam-machine` shows no error naming
     a key-encryption key it cannot open, which is what a different one
     produces for every claim (`deployment-contract.md` §The
     key-encryption key cannot be regenerated). A different
     key-encryption key is not recoverable by any step here; the only
     remedy that document names applies.
