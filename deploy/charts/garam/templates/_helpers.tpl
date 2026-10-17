{{/*
The base name every object takes, and the labels every object carries.
Kept here because a workload's Service name is what another workload
dials: `serve api` and `serve machine` reach the engine at the authz
Service, so the name has one definition rather than four.
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
{{- with (index .root.Values .component "serviceAccount").name }}
      serviceAccountName: {{ . }}
{{- end }}
{{- end -}}

{{/*
Rendered at the end of each workload's own file, so one workload's
objects stay in one file. A caller whose workload is a hook passes the
hook annotations that put its account in the same phase.
*/}}
{{- define "garam.serviceAccount" -}}
{{- $account := index .root.Values .component "serviceAccount" -}}
{{/*
Annotations reach an account this chart creates, and there is nothing
for them to reach when it creates none: under `create: false` the
account is a subject another repository owns, so an annotation written
here would be dropped without a diagnostic and the role it binds would
never arrive. Refused rather than dropped — the annotation belongs
wherever that account is created.
*/}}
{{- if and (not $account.create) $account.annotations -}}
{{- fail (printf "%s.serviceAccount.annotations is set with create: false. The chart renders no ServiceAccount under create: false, so these annotations would reach nothing: set them where that account is created, or set create: true." .component) -}}
{{- end -}}
{{- if $account.create }}
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ required (printf "%s.serviceAccount.name is required when create is true: the chart composes no name, because another repository owns the role bound to this subject (ADR-0043)" .component) $account.name }}
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
