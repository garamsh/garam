{{/*
The base name every object takes, and the labels every object carries.
Kept here because a workload's Service name is what another workload
dials: `serve api` and `serve machine` reach the policy service at its
Service, so the name has one definition rather than several.
*/}}

{{- define "garam.fullname" -}}
{{- if eq .Release.Name .Chart.Name -}}
{{- .Release.Name -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name .Chart.Name -}}
{{- end -}}
{{- end -}}

{{/*
The name of one workload's objects: the release-scoped base above with
that workload's own word after it.

The base is truncated here rather than in `garam.fullname`, because the
63-character limit applies to the whole name and every caller appends
its word after the helper returns. Truncating the base alone was a
guard that could not fire: Helm caps a release name at 53, so
`<release>-garam` reaches 59 and never met a `trunc 63`, while
`<release>-garam-machine` reached 67 and the apiserver refused it —
`helm template` renders that name and only an apply reports it.

63 is the limit that binds every object here, though for three
different reasons: a Service name is a DNS-1035 label; a Job's name is
copied into its pods' `job-name` label, and a label value is capped at
63 bytes; a Deployment name is a DNS subdomain, but the apiserver warns
above 63 because the name reaches pod hostnames.

The word is never truncated. It is what tells one workload's objects
from another's, and shortening it would collide them.
*/}}
{{- define "garam.name" -}}
{{- printf "%s-%s" (include "garam.fullname" .root | trunc (int (sub 62 (len .word))) | trimSuffix "-") .word -}}
{{- end -}}

{{- define "garam.selectorLabels" -}}
app.kubernetes.io/name: {{ .root.Chart.Name }}
app.kubernetes.io/instance: {{ .root.Release.Name }}
app.kubernetes.io/component: {{ .component }}
{{- end -}}

{{- define "garam.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .root.Chart.Name .root.Chart.Version }}
app.kubernetes.io/managed-by: {{ .root.Release.Service }}
{{ include "garam.selectorLabels" . }}
{{- end -}}

{{/*
One image reference, read from the block under `image` the caller
names. A digest is optional and renders after the tag rather than in
place of it: `repository:tag@digest` is the form a kubelet resolves by
digest, so the tag goes on naming the version a deployment chose and
the digest decides which bytes that name resolves to.
*/}}
{{- define "garam.image" -}}
{{- $image := index .root.Values.image .name -}}
{{- $repository := required (printf "image.%s.repository is required: an empty one renders `image: :<tag>`, which is not a reference any registry resolves" .name) $image.repository -}}
{{- $tag := required (printf "image.%s.tag is required: the version a deployment runs is its own to name, and a default would go on naming one release after the next lands (ADR-0039)" .name) $image.tag -}}
{{- printf "%s:%s" $repository $tag -}}
{{- with $image.digest -}}@{{ . }}{{- end -}}
{{- end -}}

{{/*
The one DSN every database-reading process takes. It names a Secret the
cluster already holds; the value never passes through this chart.
*/}}
{{- define "garam.databaseEnv" -}}
- name: GARAM_DATABASE_URL
  valueFrom:
    secretKeyRef:
      name: {{ required "database.secretName is required: it names the Secret holding the connection string" .Values.database.secretName }}
      key: {{ required "database.secretKey is required" .Values.database.secretKey }}
{{- end -}}

{{/*
The policy service's own DSN, read by `policy serve` and `policy
migrate` alike. It keeps a database of its own (ADR-0130), so it takes
no part in `database` above, and it names a Secret the cluster already
holds.
*/}}
{{- define "garam.policyDatabaseEnv" -}}
- name: POLICY_DATABASE_URL
  valueFrom:
    secretKeyRef:
      name: {{ required "policy.database.secretName is required: it names the Secret holding the policy service's connection string" .Values.policy.database.secretName }}
      key: {{ required "policy.database.secretKey is required" .Values.policy.database.secretKey }}
{{- end -}}

{{/*
The operator exchange's token endpoint (ADR-0102), read by `serve api`,
which names it in every key it issues, and by `serve machine`, which
compares every assertion's aud with it byte for byte. One value renders
both, so the two cannot disagree. Unset renders nothing, and the binary
then issues no key and answers the exchange 503.

Set, it is held to what the binary's ParseTokenEndpoint accepts: an
https URL with a host and no user information, opaque part, query or
fragment. urlParse reports none of the binary's three empty cases — a
bare `@`, `?` or `#` — so each is read off the string: an `@` before
the first `/` after `://` is user information, and a `?` or `#`
anywhere is a query or a fragment. A value net/url cannot parse at all
is refused by urlParse itself, with its own message rather than this
one.

Indented for a container's env list, the one depth both callers use,
so an unset value leaves no blank line behind.
*/}}
{{- define "garam.machineTokenUrlEnv" -}}
{{- with .Values.machine.tokenUrl }}
{{- $u := urlParse . }}
{{- if or (ne $u.scheme "https") (not $u.host) (regexMatch "^[^:]*://[^/]*@" .) $u.opaque (contains "?" .) (contains "#" .) }}
{{- fail (printf "machine.tokenUrl %q is refused: it must be an absolute https URL with a host and no user information, query or fragment, the shape serve api and serve machine accept at startup (ADR-0102)" .) }}
{{- end }}
            - name: GARAM_MACHINE_TOKEN_URL
              value: {{ . | quote }}
{{- end }}
{{- end -}}

{{/*
garam's OIDC issuer (ADR-0141), read by `serve api` and `serve machine`
from one value, so the two cannot disagree. Unset renders nothing and
the issuer stays off.

Set, it is held to what `identityIssuer` in platform/internal/cli
refuses at start, in the order it refuses: a value that differs from
the policy token issuer, no signing key file, a spelling other than
`ParseIssuer`'s one, and a value equal to the machine issuer. The
caller passes the signing key file it renders and the value that names
it, since the two workloads take theirs from different values.

`ParseIssuer` accepts only a string equal to `https://<host>/api` for
the host it parses, so that comparison covers user information, query,
fragment and a trailing slash; the other terms name the cases it lists.

Indented for a container's env list, the one depth both callers use,
so an unset value leaves no blank line behind.
*/}}
{{- define "garam.identityIssuerEnv" -}}
{{- $issuer := .root.Values.identity.issuer }}
{{- if $issuer }}
{{- $feed := .root.Values.api.policy.tokenIssuer }}
{{- if and $feed (ne $feed $issuer) }}
{{- fail (printf "identity.issuer %q and api.policy.tokenIssuer %q are both set and differ: the policy service trusts one issuer, so set both to one value (deployment-contract.md §The identity issuer is off until it is named)" $issuer $feed) }}
{{- end }}
{{- if not .keyFile }}
{{- fail (printf "identity.issuer is set and %s is not, so the issuer is off: the issuer signs with the policy signing key, and serve api and serve machine serve it only with both (deployment-contract.md §The identity issuer is off until it is named)" .keyFileValue) }}
{{- end }}
{{- $u := urlParse $issuer }}
{{- if or (ne $u.scheme "https") (not $u.host) (ne $u.host (lower $u.host)) (hasSuffix ":" $u.host) (ne $u.path "/api") (ne $issuer (printf "https://%s/api" $u.host)) }}
{{- fail (printf "identity.issuer %q is refused: it must be https, a lowercase host with no empty port, the path /api and no trailing slash, the one spelling serve api and serve machine accept at startup (ADR-0141)" $issuer) }}
{{- end }}
{{- if eq $issuer .root.Values.machine.issuer }}
{{- fail (printf "identity.issuer %q is machine.issuer: a token of one must never verify as the other, so serve refuses to start where the two are equal (ADR-0141)" $issuer) }}
{{- end }}
            - name: GARAM_IDENTITY_ISSUER
              value: {{ $issuer | quote }}
{{- end }}
{{- end -}}

{{/*
Indented for a pod spec, the one depth all five workloads call it from,
so an empty list leaves no blank line behind.
*/}}
{{- define "garam.imagePullSecrets" -}}
{{- with .Values.image.pullSecrets }}
      imagePullSecrets:
{{- range . }}
        - name: {{ . }}
{{- end }}
{{- end }}
{{- end -}}

{{/*
The two fields PodSecurity `restricted` reads on a container and nowhere
else: neither is settable on a pod spec, so every container carries its
own copy. garam binds no low port and executes nothing that gains a
capability, so dropping all of them and refusing escalation costs the
workloads nothing.

The other two fields `restricted` requires — runAsNonRoot and
seccompProfile — sit on the pod spec, beside the UID that makes
runAsNonRoot admissible for the image a workload runs.

Indented for a container in a pod spec.
*/}}
{{/*
The two fields PodSecurity `restricted` reads on a pod spec, and the
account the containers under it run as.

runAsNonRoot is not implied by a UID: the kubelet reads it as set or
unset, and with it set and no numeric runAsUser it resolves the image's
own user instead. Both images declare that user by name — `USER garam`
and `USER node` — and a name it cannot resolve to a number is refused
at container start, not at admission ("image has non-numeric user",
kubernetes v1.34.0 `pkg/kubelet/kuberuntime/security_context_others.go`).
The numbers are values under the image block the caller names, because
the account is the image's rather than the workload's and four of the
five workloads run one image.

fsGroup is the caller's own and only the machine pod passes one: it is
what gives that pod's containers a group they can read a projected
Secret under.

Indented for a pod spec.
*/}}
{{- define "garam.podSecurityContext" -}}
{{- $image := index .root.Values.image .name }}
      securityContext:
{{- with .fsGroup }}
        fsGroup: {{ . }}
{{- end }}
        runAsNonRoot: true
        runAsUser: {{ required (printf "image.%s.runAsUser is required: runAsNonRoot is asserted beside it, and the kubelet resolves the image's own user only where no number is set" .name) $image.runAsUser }}
        runAsGroup: {{ required (printf "image.%s.runAsGroup is required: the GID is the other half of the account, and a pod naming only the UID leaves it to the image" .name) $image.runAsGroup }}
        seccompProfile:
          type: RuntimeDefault
{{- end -}}

{{- define "garam.containerSecurityContext" }}
          securityContext:
            allowPrivilegeEscalation: false
            capabilities:
              drop: ["ALL"]
{{- end -}}

{{/*
The account a workload runs as. The name is a value taken verbatim, and
the chart composes none: a role another repository owns names this
subject in its trust policy, so a name composed from the release name
would move that subject whenever a deployment renamed its release, and
the failure is a credential that stops arriving at runtime (ADR-0043).
No name renders no field, and the workload runs as the namespace
default.

Indented for a pod spec, the one depth all five workloads call it from.
*/}}
{{- define "garam.serviceAccountName" -}}
{{- with (index .root.Values (default .component .valuesKey) "serviceAccount").name }}
      serviceAccountName: {{ . }}
{{- end }}
{{- end -}}

{{/*
Rendered at the end of each workload's own file, so one workload's
objects stay in one file. A caller whose workload is a hook passes the
hook annotations that put its account in the same phase. A component
whose values sit under a key other than its label, because the label
carries a hyphen a values key does not, names that key as `valuesKey`.
*/}}
{{- define "garam.serviceAccount" -}}
{{- $key := default .component .valuesKey -}}
{{- $account := index .root.Values $key "serviceAccount" -}}
{{/*
Annotations reach an account this chart creates, and there is nothing
for them to reach when it creates none: under `create: false` the
account is a subject another repository owns, so an annotation written
here would be dropped without a diagnostic and the role it binds would
never arrive. Refused rather than dropped — the annotation belongs
wherever that account is created.
*/}}
{{- if and (not $account.create) $account.annotations -}}
{{- fail (printf "%s.serviceAccount.annotations is set with create: false. The chart renders no ServiceAccount under create: false, so these annotations would reach nothing: set them where that account is created, or set create: true." $key) -}}
{{- end -}}
{{- if $account.create }}
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ required (printf "%s.serviceAccount.name is required when create is true: the chart composes no name, because another repository owns the role bound to this subject (ADR-0043)" $key) $account.name }}
  labels:
    {{- include "garam.labels" . | nindent 4 }}
  {{- if or .hookAnnotations $account.annotations }}
  annotations:
    {{- with .hookAnnotations }}
    {{- toYaml . | nindent 4 }}
    {{- end }}
    {{- with $account.annotations }}
    {{- toYaml . | nindent 4 }}
    {{- end }}
  {{- end }}
{{- end }}
{{- end -}}

{{/*
Where the token signing key is read from, in the api or the machine
container: the file the deployment writes (`signingKeyFile`), or the copy
this chart makes from the Secret `signingKey` names. Both is refused,
since a file the deployment writes and one the pod copies would disagree
on what the process reads. Takes the policy values (`policy`) and the
name they sit under (`value`, such as `api.policy`).
*/}}
{{- define "garam.policySigningKeyFile" -}}
{{- $k := .policy.signingKey -}}
{{- if and $k.secretName .policy.signingKeyFile -}}
{{- fail (printf "%s.signingKey.secretName and %s.signingKeyFile are both set: the pod copies the key from the Secret to its own file, so name one source" .value .value) -}}
{{- end -}}
{{- if $k.secretName -}}
{{- printf "%s/%s" (include "garam.policySigningDir" .) (required (printf "%s.signingKey.key is required beside %s.signingKey.secretName" .value .value) $k.key) -}}
{{- else -}}
{{- .policy.signingKeyFile -}}
{{- end -}}
{{- end -}}

{{/*
The directory the signing key's copy lands in, in the api and the
machine container alike.
*/}}
{{- define "garam.policySigningDir" -}}
/run/garam/policy-signing
{{- end -}}

{{/*
The signing key as the files an owner-only copy is made of: one entry
when `signingKey.secretName` is set, none when it is not, as a YAML
list. This is the one place that decides whether a pod copies the key;
the helpers below render nothing for an empty list. Takes `policy`.
*/}}
{{- define "garam.policySigningKeySources" -}}
{{- $k := .policy.signingKey -}}
{{- if $k.secretName -}}
{{- list (dict "secretName" $k.secretName "key" $k.key) | toYaml -}}
{{- else -}}
[]
{{- end -}}
{{- end -}}

{{/*
A process that refuses a key file it does not own, or that any group or
other bit is set on (ADR-0015), cannot read a projected Secret, which is
root-owned. These three helpers give it a copy: a projected volume of
exactly the keys named, an init container that copies each with
`install -m <mode>` as the pod's own account onto a memory-backed
`emptyDir`, so the copy never reaches the node's disk, and the mount of
that directory in the container that reads it. The pod's `fsGroup` is
what lets the copy read the `0440` projection.

Each takes `name` (the init container and the directory's volume; the
Secret's volume is `<name>-secret`), `dest` (where the container reads
the copy), `sources` (a list of `secretName` and `key`, one file each,
so a Secret carrying more projects only what is named) and, for the
init container, `source` (where it reads the projection), `mode`,
`root` and `resources`. All three render nothing for an empty `sources`,
which is how a copy that is switched off by a value vanishes from a
pod; with `header`, the init container also writes the pod's
`initContainers:` key, for a pod with no other.

The mode is the consumer's: the machine listener's key material and the
policy service's pair are installed 0600, as they were before these
helpers, and the token signing key 0400, which a process that only
reads the key once at start needs no more than. Nothing in the binary
asks for a mode beyond ADR-0015's no group or other bit, so this is a
choice of each caller rather than a rule of this helper.

Indented for a pod's initContainers list, a container's volumeMounts
list and a pod's volumes list respectively.
*/}}
{{- define "garam.ownerOnlyInit" -}}
{{- if .sources }}
{{- if .header }}
      initContainers:
{{- end }}
{{- $keys := list }}
{{- range .sources }}{{ $keys = append $keys .key }}{{ end }}
        - name: {{ .name }}
          image: {{ required "machine.copyImage is required" .root.Values.machine.copyImage }}
{{- include "garam.containerSecurityContext" .root }}
          command: ["/bin/sh", "-ec"]
          args:
            - |
              for f in {{ join " " $keys }}; do
                install -m {{ .mode }} "{{ .source }}/$f" "{{ .dest }}/$f"
              done
              ls -l {{ .dest }}
          volumeMounts:
            - name: {{ .name }}-secret
              mountPath: {{ .source }}
              readOnly: true
            - name: {{ .name }}
              mountPath: {{ .dest }}
          resources: {{- toYaml .resources | nindent 12 }}
{{- end }}
{{- end -}}

{{- define "garam.ownerOnlyMount" }}
{{- if .sources }}
            - name: {{ .name }}
              mountPath: {{ .dest }}
              readOnly: true
{{- end }}
{{- end -}}

{{- define "garam.ownerOnlyVolumes" }}
{{- if .sources }}
        - name: {{ .name }}-secret
          projected:
            defaultMode: 0440
            sources:
{{- range .sources }}
              - secret:
                  name: {{ .secretName }}
                  items:
                    - key: {{ .key }}
                      path: {{ .key }}
{{- end }}
        - name: {{ .name }}
          emptyDir:
            medium: Memory
            sizeLimit: 1Mi
{{- end }}
{{- end -}}

{{/*
How many replicas a workload runs, and how an upgrade replaces them
(ADR-0178). Takes the workload's values key (`workload`: api, machine,
policy, web or site). The count is a value so that a deployment with one
schedulable Node sets one; zero is refused, since a Deployment that runs
no pod is not one this chart describes. `RollingUpdate` with
`maxUnavailable: 0` and `maxSurge: 1` brings a new pod up before an old
one goes, so an upgrade is a handover and not a gap, at the price of the
version-skew rule the ADR states. Indented for a Deployment spec.
*/}}
{{- define "garam.replicasAndRollout" -}}
{{- $n := int (index .root.Values .workload).replicas -}}
{{- if lt $n 1 }}
{{- fail (printf "%s.replicas %d is refused: a workload runs at least one replica (ADR-0178)" .workload $n) }}
{{- end }}
  replicas: {{ $n }}
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 0
      maxSurge: 1
{{- end -}}

{{/*
Spreads a workload's pods over Nodes: no Node holds more than one pod
more than another (`maxSkew: 1`), and a pod that cannot be placed stays
Pending rather than doubling up (`DoNotSchedule`), so the loss of one
Node leaves the other's pod serving (ADR-0178). On one Node there is one
domain and both pods schedule. Takes `root` and `component`. Indented for
a pod spec.
*/}}
{{- define "garam.topologySpread" }}
      topologySpreadConstraints:
        - maxSkew: 1
          topologyKey: kubernetes.io/hostname
          whenUnsatisfiable: DoNotSchedule
          labelSelector:
            matchLabels:
              {{- include "garam.selectorLabels" . | nindent 14 }}
{{- end -}}

{{/*
A PodDisruptionBudget that lets one pod of the workload be drained at a
time. `maxUnavailable: 1` and not `minAvailable: 1`: a deployment that
sets one replica would otherwise have every drain blocked for good
(ADR-0178). It covers a voluntary eviction only. Takes `root` and
`component`, which is also the word the object is named after.
*/}}
{{- define "garam.podDisruptionBudget" -}}
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: {{ include "garam.name" (dict "root" .root "word" .component) }}
  labels:
    {{- include "garam.labels" . | nindent 4 }}
spec:
  maxUnavailable: 1
  selector:
    matchLabels:
      {{- include "garam.selectorLabels" . | nindent 6 }}
{{- end -}}
