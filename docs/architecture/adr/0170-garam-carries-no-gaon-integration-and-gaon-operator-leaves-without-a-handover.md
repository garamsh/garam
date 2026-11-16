# ADR 0170: garam carries no gaon integration, and `gaon-operator/` leaves without a handover

> Status: accepted; built (#1946)
> Date: 2026-11-16

Drafted for #1946, which reserves this number. gaon was discontinued
on 2026-11-13: decisions 18 and 19 on garamsh/aurora#749 and aurora's
ADR 0049. It supersedes
[ADR-0151](./0151-the-gaon-operator-is-a-service-here-that-runs-each-managed-agent-as-one-microvm.md)
in whole,
[ADR-0153](./0153-the-gaon-guest-runs-as-root-and-its-supervisor-drops-each-program-to-the-account-it-needs.md),
[ADR-0154](./0154-the-gaon-operator-registers-each-placement-and-renders-the-agents-configuration-into-its-microvm.md)
and
[ADR-0155](./0155-garam-ships-a-second-helm-chart-that-renders-the-gaon-kinds.md)
in whole, and
[ADR-0158](./0158-garam-carries-no-gaon-operator-and-a-release-publishes-six-images.md)
in part: item 3's handover condition and item 4.

## Context

- **The platform gaon ran on is gone.** The operator, the chart and the
  documents describing a gaon deployment (ADR-0151, ADR-0153 to
  ADR-0155) existed to run garam and its agents on a gaon cluster. With
  gaon discontinued nothing can run them, and keeping them costs a
  module in `go.work`, a row in `tooling/ci/modules`, a dependabot
  entry, a chart in `make -C deploy/charts ci` and a harness in
  `deploy/local`, each checked on every change.
- **ADR-0158 already decided garam carries no gaon operator**, and
  made deleting `gaon-operator/` wait until
  garamsh/garam-agent-operator held the directory at a commit passing
  its own checks. That did not happen, and with gaon gone the operator
  has no one to receive it.
- **What is not gaon's stays.** An operator of any kind holds a
  garam-issued key, exchanges its assertion for a token and calls the
  machine listener ([operators.md](../operators.md)). ADR-0152 logs
  garam in to OpenBao with a workload identity file; its mechanism
  names no platform, and it is built in
  `platform/internal/message/transit`.

## Decision

**garam carries no gaon integration.** The operator, the chart, the
pair-run harness, their CI and dependabot entries and the documents
describing a gaon deployment leave the repository.

1. **`gaon-operator/` is deleted without the handover ADR-0158 waited
   for.** The code stays in git history. ADR-0158 item 3's condition,
   that the directory leaves only after garamsh/garam-agent-operator
   holds it, no longer stands; ADR-0158 item 1 (garam publishes no
   gaon operator) and item 2 (a release publishes six images) stand.
2. **`deploy/charts/garam-gaon/` is deleted**, with its `make -C
   deploy/charts ci` targets and `deployment-gaon.md`. ADR-0158 item 4,
   which kept the chart, no longer stands. A deployment target is
   decided anew once the owner has chosen one.
3. **`deploy/local/docker-compose.pair-run.yml` is deleted** with the
   harness it belonged to.
4. **What an operator of any kind calls stays:** the machine API and
   the key it issues.
5. **What this supersedes.**
   - ADR-0151 in whole: it decided the operator, and nothing of it
     remains.
   - ADR-0153 and ADR-0154 in whole: each decides the operator's guest
     and registration, built only in `gaon-operator/`, and ADR-0158
     named both as leaving with ADR-0151.
   - ADR-0155 in whole: it decided the chart that rendered the gaon
     kinds, built only in `deploy/charts/garam-gaon/`.
   - ADR-0158 in part, as above.
   - ADR-0156 is already superseded by ADR-0158 and stays so.
   - **ADR-0152 is not superseded.** It decides a login form garam
     implements and tests without any gaon object, so it stays
     `accepted`. Its text that names gaon as the identity's writer is
     the record of why it was written.

ADRs are not deleted: each superseded one keeps its body and takes a
status naming this one.

## Consequences

- `go.work`, `tooling/ci/modules`, `.github/dependabot.yml` and
  `deploy/charts/Makefile` name no gaon path, and every Go entry point
  covers one module fewer.
- [operators.md](../operators.md), [agents.md](../agents.md),
  [structure.md](../structure.md), [deployment-contract.md](../deployment-contract.md),
  [deployment-needs.md](../deployment-needs.md) and the index describe
  no gaon deployment.
- No published image or ECR repository is deleted: no release
  published a gaon image since ADR-0158.
- A deployment target and a MicroVM operator, if wanted, are decided in
  a new ADR.

## Signs the decision was wrong

- A deployment target is chosen that needs what `deploy/charts/garam-gaon/`
  or `gaon-operator/` held, and the history is read for it more than
  once: the removal was premature in kind, not only in time.
- A reader of `deployment-contract.md` finds the Transit identity file
  without any account of what writes it and asks for it: the document
  needed a writer named.
