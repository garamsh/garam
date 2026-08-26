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

{{- define "garam.serverImage" -}}
{{ .Values.image.server.repository }}:{{ required "image.server.tag is required: no release exists, so no default names an image that can be pulled (ADR-0039)" .Values.image.server.tag }}
{{- end -}}

{{- define "garam.webImage" -}}
{{ .Values.image.web.repository }}:{{ required "image.web.tag is required: no release exists, so no default names an image that can be pulled (ADR-0039)" .Values.image.web.tag }}
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
