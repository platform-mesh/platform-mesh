{{- define "access-vw.labels" -}}
app: access-vw
app.kubernetes.io/name: access-vw
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
{{- end }}

{{/*
The providers, validated: rbac, openfga, or both separated by a comma and
without spaces, in the order given.
*/}}
{{- define "access-vw.provider" -}}
{{- $providers := splitList "," (.Values.provider | replace " " "") -}}
{{- range $providers -}}
{{- if not (has . (list "rbac" "openfga")) -}}
{{- fail (printf "provider must be rbac, openfga or both separated by a comma, got %q" $.Values.provider) -}}
{{- end -}}
{{- end -}}
{{ join "," ($providers | uniq) }}
{{- end }}

{{/*
gRPC address of OpenFGA: explicit value or the openfga Service of the release
namespace.
*/}}
{{- define "access-vw.openfgaAddr" -}}
{{ .Values.openfga.addr | default (printf "openfga.%s.svc.cluster.local:8081" .Release.Namespace) }}
{{- end }}

{{/*
External hostname of the kcp front-proxy: explicit value or kcp.api.<exposure.baseDomain>.
*/}}
{{- define "access-vw.externalHostname" -}}
{{- if .Values.external.hostname -}}
{{ .Values.external.hostname }}
{{- else -}}
{{ printf "kcp.api.%s" (required "external.hostname or exposure.baseDomain is required" .Values.exposure.baseDomain) }}
{{- end -}}
{{- end }}

{{/*
External port of the kcp front-proxy: explicit value or exposure.port.
*/}}
{{- define "access-vw.externalPort" -}}
{{ .Values.external.port | default .Values.exposure.port }}
{{- end }}

{{/*
Base URL for per-cluster endpoints in SCAR responses: explicit value or
/clusters/ at the external front-proxy address.
*/}}
{{- define "access-vw.endpointBase" -}}
{{ .Values.server.endpointBase | default (printf "https://%s:%s/clusters/" (include "access-vw.externalHostname" .) (include "access-vw.externalPort" .)) }}
{{- end }}
