{{- define "common.entity.name" -}}
{{- if contains .Chart.Name .Release.Name }}
{{- printf "%s" .Chart.Name | trunc 63 }}
{{- else }}
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 63 }}
{{- end -}}
{{- end -}}

{{- define "release.name" -}}
{{- printf "%s" .Release.Name | trunc 63 }}
{{- end -}}

{{/* Adds the optional clusterScopedName.prefix and .suffix to a cluster-scoped name so several installations can share a cluster. */}}
{{- define "common.clusterScoped.affix" -}}
{{- $name := .name -}}
{{- $prefix := include "common.getKeyValue" (dict "Values" .context.Values "key" "clusterScopedName.prefix") -}}
{{- $suffix := include "common.getKeyValue" (dict "Values" .context.Values "key" "clusterScopedName.suffix") -}}
{{- if $prefix }}{{ $name = printf "%s-%s" $prefix $name }}{{ end -}}
{{- if $suffix }}{{ $name = printf "%s-%s" $name $suffix }}{{ end -}}
{{- $name -}}
{{- end -}}

{{- define "common.clusterScoped.name" -}}
{{- include "common.clusterScoped.affix" (dict "context" . "name" (include "common.entity.name" .)) -}}
{{- end -}}
