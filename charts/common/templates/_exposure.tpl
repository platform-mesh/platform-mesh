{{/*
Construct a public hostname from exposure.baseDomain and an optional prefix.
Usage: include "common.exposure.host" (dict "exposure" .Values.exposure "prefix" "root.kcp")
An empty base domain produces an empty hostname for standalone charts.
*/}}
{{- define "common.exposure.host" -}}
{{- $exposure := .exposure | default dict -}}
{{- if $exposure.baseDomain -}}
  {{- if .prefix -}}
    {{- printf "%s.%s" .prefix $exposure.baseDomain -}}
  {{- else -}}
    {{- $exposure.baseDomain -}}
  {{- end -}}
{{- end -}}
{{- end -}}

{{/*
Construct a public URL, omitting only the protocol's standard port.
Usage: include "common.exposure.url" (dict "exposure" .Values.exposure "path" "/keycloak")
Optional prefix and path belong to the chart, not the operator contract.
*/}}
{{- define "common.exposure.url" -}}
{{- $exposure := .exposure | default dict -}}
{{- $host := include "common.exposure.host" . -}}
{{- if $host -}}
  {{- $protocol := $exposure.protocol | default "https" -}}
  {{- $host = include "common.exposure.authority" . -}}
  {{- printf "%s://%s%s" $protocol $host (.path | default "") -}}
{{- end -}}
{{- end -}}

{{/*
Return hostnames as YAML: a nonempty explicit list, otherwise the base and wildcard.
Usage: include "common.exposure.hostnames" . | fromYamlArray
Without a base domain or explicit hostnames, return an empty list.
*/}}
{{- define "common.exposure.hostnames" -}}
{{- $exposure := .Values.exposure | default dict -}}
{{- $hostnames := $exposure.hostnames | default list -}}
{{- if hasKey . "hostnames" -}}
  {{- $hostnames = .hostnames | default list -}}
{{- end -}}
{{- if $hostnames -}}
  {{- toYaml $hostnames -}}
{{- else if $exposure.baseDomain -}}
  {{- toYaml (list (include "common.exposure.host" (dict "exposure" $exposure)) (include "common.exposure.host" (dict "exposure" $exposure "prefix" "*"))) -}}
{{- else -}}
  {{- toYaml (list) -}}
{{- end -}}
{{- end -}}

{{/* Host and protocol-aware port without the scheme, for applications accepting a domain:port. */}}
{{- define "common.exposure.authority" -}}
{{- $exposure := .exposure | default dict -}}
{{- $host := include "common.exposure.host" . -}}
{{- if $host -}}
  {{- $protocol := $exposure.protocol | default "https" -}}
  {{- $port := $exposure.port | default 443 | int -}}
  {{- if not (or (and (eq $protocol "http") (eq $port 80)) (and (eq $protocol "https") (eq $port 443))) -}}
    {{- $host = printf "%s:%d" $host $port -}}
  {{- end -}}
  {{- $host -}}
{{- end -}}
{{- end -}}
