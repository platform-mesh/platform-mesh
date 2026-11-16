{{- define "kcp.externalHostname" -}}
{{- .Values.kcp.external.hostname | default (include "common.exposure.host" (dict "exposure" .Values.exposure "prefix" "kcp.api")) -}}
{{- end -}}

{{- define "kcp.externalPort" -}}
{{- if .Values.kcp.external.port -}}
  {{- .Values.kcp.external.port -}}
{{- else if .Values.exposure.baseDomain -}}
  {{- .Values.exposure.port -}}
{{- else -}}
  8443
{{- end -}}
{{- end -}}

{{/* Contract domains are the platform domain; standalone charts retain the old external-host fallback. */}}
{{- define "kcp.rootHostname" -}}
{{- if .Values.kcp.rootShard.hostname -}}
  {{- .Values.kcp.rootShard.hostname -}}
{{- else if .Values.exposure.baseDomain -}}
  {{- include "common.exposure.host" (dict "exposure" .Values.exposure "prefix" "root.kcp") -}}
{{- else -}}
  {{- printf "root.kcp.%s" (include "kcp.externalHostname" .) -}}
{{- end -}}
{{- end -}}

{{- define "kcp.shardHostname" -}}
{{- if .shard.hostname -}}
  {{- .shard.hostname -}}
{{- else if .root.Values.exposure.baseDomain -}}
  {{- include "common.exposure.host" (dict "exposure" .root.Values.exposure "prefix" (printf "%s.kcp" .shard.name)) -}}
{{- else -}}
  {{- printf "%s.kcp.%s" .shard.name (include "kcp.externalHostname" .root) -}}
{{- end -}}
{{- end -}}

{{/* KCP always uses TLS, regardless of the public application protocol. */}}
{{- define "kcp.shardBaseURL" -}}
{{- if .root.Values.exposure.baseDomain -}}
{{- include "common.exposure.url" (dict "exposure" (dict "baseDomain" .hostname "protocol" "https" "port" (include "kcp.externalPort" .root)) "path" "/") -}}
{{- else -}}
{{- printf "https://%s:%s/" .hostname (include "kcp.externalPort" .root) -}}
{{- end -}}
{{- end -}}
