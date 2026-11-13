{{/*
The half of the deployment this render holds. Every template file asks
for it first, so a missing or unknown value refuses the render before any
object is written.
*/}}
{{- define "garam-gaon.stage" -}}
{{- if not (has .Values.stage (list "migrate" "serve")) -}}
{{- fail (printf "stage must be migrate or serve, got %q: gaon applies a stream in the order Helm emits and Helm emits Deployments before Jobs, so one render holds one half of the deployment (README §Order)" .Values.stage) -}}
{{- end -}}
{{- .Values.stage -}}
{{- end -}}

{{/*
An object's name, `<namePrefix>-<word>`. A Service name is a DNS label of
at most 63 characters that starts with a letter, and a replica or a Job's
MicroVM appends `-<index>`, so a name is held to 60.
*/}}
{{- define "garam-gaon.name" -}}
{{- $prefix := toString .root.Values.namePrefix -}}
{{- if not (regexMatch "^[a-z]([a-z0-9-]*[a-z0-9])?$" $prefix) -}}
{{- fail (printf "namePrefix %q is refused: it is the start of every object's name, a DNS label that starts with a letter" $prefix) -}}
{{- end -}}
{{- $name := printf "%s-%s" $prefix .word -}}
{{- if gt (len $name) 60 -}}
{{- fail (printf "namePrefix %q makes the name %q longer than 60 characters: a Service name is held to 63 and a MicroVM's name adds an index" $prefix $name) -}}
{{- end -}}
{{- $name -}}
{{- end -}}

{{/*
A migration Job's name. A finished Job and its MicroVM are kept, so the
name carries the run.
*/}}
{{- define "garam-gaon.jobName" -}}
{{- $run := required "migrate.run is required: a finished Job keeps its name, so each run of a migration is a Job of a new name" .root.Values.migrate.run -}}
{{- if not (regexMatch "^[a-z0-9]([a-z0-9-]*[a-z0-9])?$" (toString $run)) -}}
{{- fail (printf "migrate.run %q is refused: it is part of a name, lowercase letters, digits and hyphens" $run) -}}
{{- end -}}
{{- include "garam-gaon.name" (dict "root" .root "word" (printf "%s-%s" .word $run)) -}}
{{- end -}}

{{/*
A Service's full name, the only form gaon resolves: no search line is
written, so a short name is not.
*/}}
{{- define "garam-gaon.host" -}}
{{- $zone := toString .root.Values.dnsZone -}}
{{- if not (regexMatch "^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$" $zone) -}}
{{- fail (printf "dnsZone %q is refused: it is the zone of every Service name, lowercase labels joined by dots" $zone) -}}
{{- end -}}
{{- printf "%s.default.svc.%s" (include "garam-gaon.name" .) $zone -}}
{{- end -}}

{{- define "garam-gaon.selector" -}}
app.kubernetes.io/name: {{ include "garam-gaon.name" . }}
{{- end -}}

{{- define "garam-gaon.image" -}}
{{- $i := index .root.Values.image .image -}}
{{- $repository := required (printf "image.%s.repository is required" .image) $i.repository -}}
{{- if not (regexMatch "^sha256:[0-9a-f]{64}$" (toString $i.digest)) -}}
{{- fail (printf "image.%s.digest must be sha256: and 64 hexadecimal digits, got %q: gaon refuses an image a digest does not pin" .image $i.digest) -}}
{{- end -}}
{{- printf "%s@%s" $repository $i.digest -}}
{{- end -}}

{{- define "garam-gaon.user" -}}
{{- $i := index .root.Values.image .image -}}
{{- if not (regexMatch "^[0-9]+:[0-9]+$" (toString $i.user)) -}}
{{- fail (printf "image.%s.user must be uid:gid, both numbers, got %q: a file that names no owner takes the MicroVM's user only in that form" .image $i.user) -}}
{{- end -}}
{{- $i.user -}}
{{- end -}}

{{/*
One field of a reference to a Secret or a ConfigMap the deployment
creates. The chart passes no value through.
*/}}
{{- define "garam-gaon.ref" -}}
{{- required (printf "%s.%s is required: it names an object the deployment creates, and no value passes through this chart" .path .field) (index .ref .field) -}}
{{- end -}}

{{- define "garam-gaon.env" -}}
- name: {{ .name }}
  value: {{ .value | toString | quote }}
{{- end -}}

{{- define "garam-gaon.envSecret" -}}
- name: {{ .name }}
  valueFrom:
    secretKeyRef:
      name: {{ include "garam-gaon.ref" (dict "path" .path "ref" .ref "field" "secret") | quote }}
      key: {{ include "garam-gaon.ref" (dict "path" .path "ref" .ref "field" "key") | quote }}
{{- end -}}

{{/* A key file: owner-only, owned by the MicroVM's user. */}}
{{- define "garam-gaon.fileSecret" -}}
- path: {{ .file }}
  mode: 0400
  valueFrom:
    secretKeyRef:
      name: {{ include "garam-gaon.ref" (dict "path" .path "ref" .ref "field" "secret") | quote }}
      key: {{ include "garam-gaon.ref" (dict "path" .path "ref" .ref "field" "key") | quote }}
{{- end -}}

{{/* A public file, in gaon's default mode. */}}
{{- define "garam-gaon.fileConfigMap" -}}
- path: {{ .file }}
  valueFrom:
    configMapKeyRef:
      name: {{ include "garam-gaon.ref" (dict "path" .path "ref" .ref "field" "configMap") | quote }}
      key: {{ include "garam-gaon.ref" (dict "path" .path "ref" .ref "field" "key") | quote }}
{{- end -}}

{{/*
The machine token URL, as serve api and serve machine accept it at
startup (ADR-0102): an absolute https URL with a host and no user
information, query or fragment. Empty renders nothing.
*/}}
{{- define "garam-gaon.machineTokenUrl" -}}
{{- with .Values.machine.tokenUrl -}}
{{- $u := urlParse . -}}
{{- if or (ne $u.scheme "https") (not $u.host) (regexMatch "^[^:]*://[^/]*@" .) $u.opaque (contains "?" .) (contains "#" .) -}}
{{- fail (printf "machine.tokenUrl %q is refused: it must be an absolute https URL with a host and no user information, query or fragment" .) -}}
{{- end -}}
{{- . -}}
{{- end -}}
{{- end -}}
