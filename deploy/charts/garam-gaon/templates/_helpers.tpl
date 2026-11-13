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
{{- $run := toString (required "migrate.run is required: a finished Job keeps its name, so each run of a migration is a Job of a new name" .root.Values.migrate.run) -}}
{{- if not (regexMatch "^[a-z0-9]([a-z0-9-]*[a-z0-9])?$" $run) -}}
{{- fail (printf "migrate.run %q is refused: it is part of a name, lowercase letters, digits and hyphens, and a number too long to write as one is read as 2.02e+07, so a run that is only digits is quoted" $run) -}}
{{- end -}}
{{- include "garam-gaon.name" (dict "root" .root "word" (printf "%s-%s" .word $run)) -}}
{{- end -}}

{{/*
A Service's full name, the only form gaon resolves: no search line is
written, so a short name is not.
*/}}
{{- define "garam-gaon.host" -}}
{{- $zone := toString .root.Values.dnsZone -}}
{{- if not (regexMatch "^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$" $zone) -}}
{{- fail (printf "dnsZone %q is refused: it is the zone of every Service name, lowercase labels joined by single dots" $zone) -}}
{{- end -}}
{{- printf "%s.default.svc.%s" (include "garam-gaon.name" .) $zone -}}
{{- end -}}

{{- define "garam-gaon.selector" -}}
app.kubernetes.io/name: {{ include "garam-gaon.name" . }}
{{- end -}}

{{- define "garam-gaon.image" -}}
{{- $i := index .root.Values.image .image -}}
{{- $repository := required (printf "image.%s.repository is required" .image) $i.repository -}}
{{- if not (regexMatch "^[^@:]+(:[0-9]+)?(/[^@:]+)*$" (toString $repository)) -}}
{{- fail (printf "image.%s.repository must hold no tag and no digest, got %q: the chart adds @<digest> and gaon accepts <repository>@sha256:<digest> only, so a tag in the repository is refused when the MicroVM is created" .image $repository) -}}
{{- end -}}
{{- if not (regexMatch "^sha256:[0-9a-f]{64}$" (toString $i.digest)) -}}
{{- fail (printf "image.%s.digest must be sha256: and 64 hexadecimal digits, got %q: gaon refuses an image a digest does not pin" .image $i.digest) -}}
{{- end -}}
{{- printf "%s@%s" $repository $i.digest | quote -}}
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

{{/*
A quantity as gaon reads one (lib/api quantity.go): a decimal number and
an optional m, k, M, G, T, Ki, Mi, Gi or Ti. The message opens with the
value's dotted name.
*/}}
{{- define "garam-gaon.quantity" -}}
{{- $v := toString (required (printf "%s is required: gaon's node agent refuses a MicroVM whose spec names no cpu or no memory, and the chart holds no figure for any process" .path) .value) -}}
{{- if not (regexMatch "^[0-9]+(\\.[0-9]+)?(m|k|M|G|T|Ki|Mi|Gi|Ti)?$" $v) -}}
{{- fail (printf "%s must be a number with an optional m, k, M, G, T, Ki, Mi, Gi or Ti, got %q: gaon decodes no other form of a quantity" .path $v) -}}
{{- end -}}
{{- $v -}}
{{- end -}}

{{/*
One object's `resources`. The requests are required and have no default:
the scheduler places a MicroVM by them, and the node agent sizes it from
the limits, else the requests, and fails a MicroVM that names no cpu or
no memory (node-agent/internal/microvms/microvms.go at the commit the
README names). No figure is written in this chart; the deployment
supplies each one. The limits are optional.
*/}}
{{- define "garam-gaon.resources" -}}
{{- $path := printf "resources.%s" .key -}}
{{- $r := index .root.Values.resources .key -}}
resources:
  requests:
    cpu: {{ include "garam-gaon.quantity" (dict "path" (printf "%s.requests.cpu" $path) "value" $r.requests.cpu) | quote }}
    memory: {{ include "garam-gaon.quantity" (dict "path" (printf "%s.requests.memory" $path) "value" $r.requests.memory) | quote }}
  {{- if or $r.limits.cpu $r.limits.memory }}
  limits:
    {{- with $r.limits.cpu }}
    cpu: {{ include "garam-gaon.quantity" (dict "path" (printf "%s.limits.cpu" $path) "value" .) | quote }}
    {{- end }}
    {{- with $r.limits.memory }}
    memory: {{ include "garam-gaon.quantity" (dict "path" (printf "%s.limits.memory" $path) "value" .) | quote }}
    {{- end }}
  {{- end }}
{{- end -}}

{{/*
The identity issuer, or nothing where it is off. `serve api` and
`serve machine` serve it only with both GARAM_IDENTITY_ISSUER and the
policy signing key set (platform/internal/cli/identity.go), and refuse to
start where it is not `https://<lowercase host>/api`, is not the
policy.tokenIssuer, or is the machine issuer. The service keys file is
read by `serve machine`, which derives the introspection URL from the
token URL, and that must end /oauth/token. Each is refused here first,
with the value's name.
*/}}
{{- define "garam-gaon.identityIssuer" -}}
{{- $id := .Values.identity -}}
{{- $keys := $id.serviceKeys -}}
{{- $issuer := toString $id.issuer -}}
{{- if and (or $keys.configMap $keys.key) (not (and $keys.configMap $keys.key)) -}}
{{- fail "identity.serviceKeys.configMap and identity.serviceKeys.key are set together: the file is one key of one ConfigMap" -}}
{{- end -}}
{{- if and (not $issuer) (or $keys.configMap $keys.key) -}}
{{- fail "identity.issuer is required once identity.serviceKeys is set: the chart renders the signing key on the machine listener only with the issuer, and the exchange the keys serve answers only with it" -}}
{{- end -}}
{{- if $issuer -}}
{{- if not (regexMatch "^https://[a-z0-9]([a-z0-9.-]*[a-z0-9])?(:[0-9]+)?/api$" $issuer) -}}
{{- fail (printf "identity.issuer must be https://<lowercase host>/api with no trailing slash and no empty port, got %q: a verifier compares it byte for byte against every token's iss" $issuer) -}}
{{- end -}}
{{- if eq $issuer (toString .Values.machine.issuer) -}}
{{- fail (printf "identity.issuer is machine.issuer, %q: serve refuses to start where the two are equal, so a token of one never verifies as the other" $issuer) -}}
{{- end -}}
{{- if ne $issuer (toString .Values.policy.tokenIssuer) -}}
{{- fail (printf "identity.issuer %q differs from policy.tokenIssuer %q: the policy service trusts one issuer, and serve refuses to start with the two set and different" $issuer .Values.policy.tokenIssuer) -}}
{{- end -}}
{{- end -}}
{{- if or $keys.configMap $keys.key -}}
{{- $url := toString .Values.machine.tokenUrl -}}
{{- if not $url -}}
{{- fail "machine.tokenUrl is required once identity.serviceKeys is set: serve machine derives the introspection URL from it" -}}
{{- end -}}
{{- if not (hasSuffix "/oauth/token" $url) -}}
{{- fail (printf "machine.tokenUrl must end in /oauth/token once identity.serviceKeys is set, got %q: serve machine replaces that tail to name its introspection endpoint" $url) -}}
{{- end -}}
{{- end -}}
{{- $issuer -}}
{{- end -}}
