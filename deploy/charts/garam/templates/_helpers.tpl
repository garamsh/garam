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
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
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
