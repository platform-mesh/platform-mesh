{{- /*
Render hostAliases using lookup order:
1. hostAliasesOverride
2. global.hostAliases
3. hostAliases
4. defaults.hostAliases

Each of these keys is expected to be an array, or a map with "enabled" and "entries" keys.
If the chosen one is empty or unset, render nothing.
*/ -}}
{{- define "common.hostAliases" -}}
{{- $v := .Values -}}
{{- $source := dict -}}
{{- $aliases := list -}}
{{- $aliasesEnabled := false -}}
{{- $defaultKey := "common.defaults.hostAliases" -}}

{{- if and $v (hasKey $v "hostAliasesOverride") -}}
  {{- $source = index $v "hostAliasesOverride" -}}
{{- else if and $v.global (hasKey $v.global "hostAliases") -}}
  {{- $source = index $v.global "hostAliases" -}}
{{- else if and $v (hasKey $v "hostAliases") -}}
  {{- $source = index $v "hostAliases" -}}
{{- else if eq (include "common.hasNestedKey" (dict "Values" $v "key" $defaultKey)) "true" }}
  {{- $source = index $v.common.defaults "hostAliases" -}}
{{- end }}

{{- if $source -}}
  {{- if kindIs "slice" $source -}}
    {{- $aliases = $source -}}
  {{- else if and (kindIs "map" $source) (default true $source.enabled) -}}
    {{- $aliases = $source.entries | default $v.common.defaults.hostAliases.entries -}}
    {{- $aliasesEnabled = $source.enabled -}}
  {{- end -}}
{{- end -}}

{{- if $aliasesEnabled -}}
hostAliases: {{ toYaml $aliases | nindent 2 }}
{{- end -}}
{{- end }}


{{- define "common.hostAliasesEnabled" -}}
{{- $v := .Values -}}
{{- $source := dict -}}
{{- $aliases := list -}}
{{- $aliasesEnabled := false -}}
{{- $defaultKey := "common.defaults.hostAliases" -}}

{{- if and $v (hasKey $v "hostAliasesOverride") -}}
  {{- $source = index $v "hostAliasesOverride" -}}
{{- else if and $v.global (hasKey $v.global "hostAliases") -}}
  {{- $source = index $v.global "hostAliases" -}}
{{- else if and $v (hasKey $v "hostAliases") -}}
  {{- $source = index $v "hostAliases" -}}
{{- else if eq (include "common.hasNestedKey" (dict "Values" $v "key" $defaultKey)) "true" }}
  {{- $source = index $v.common.defaults "hostAliases" -}}
{{- end }}

{{- if $source -}}
  {{- if kindIs "slice" $source -}}
    {{- $aliases = $source -}}
  {{- else if and (kindIs "map" $source) (default true $source.enabled) -}}
    {{- $aliases = $source.entries | default $v.common.defaults.hostAliases.entries -}}
    {{- $aliasesEnabled = $source.enabled -}}
  {{- end -}}
{{- end -}}

{{ toYaml $aliasesEnabled }}
{{- end }}
