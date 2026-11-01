{{/* 
  Function: common.getKeyValue
  Description: 
    Retrieves a value from values.yaml by checking keys in this order: 
    1. Override key, 2. Global key, 3. Chart key ,4. Default key. Returns an empty string if none exist.

  Parameters:
    - .key: Key path to lookup.
    - .Values: Values object.
*/}}
{{- define "common.getKeyValue" -}}
  {{- $keyPath := .key -}}
  {{- $values := .Values -}}

  {{- $overrideKey := printf "%sOverride" $keyPath -}}
  {{- $globalKey := printf "global.%s" $keyPath -}}
  {{- $defaultKey := printf "common.defaults.%s" $keyPath -}}

  {{- $value := "" -}}
  {{- if eq (include "common.hasNestedKey" (dict "Values" $values "key" $overrideKey)) "true" }}
    {{- $value = include "common.getNestedValue" (dict "Values" $values "key" $overrideKey) }}
  {{- else if eq (include "common.hasNestedKey" (dict "Values" $values "key" $globalKey)) "true" }}
    {{- $value = include "common.getNestedValue" (dict "Values" $values "key" $globalKey) }}
  {{- else if eq (include "common.hasNestedKey" (dict "Values" $values "key" $keyPath)) "true" }}
    {{- $value = include "common.getNestedValue" (dict "Values" $values "key" $keyPath) }}
  {{- else if eq (include "common.hasNestedKey" (dict "Values" $values "key" $defaultKey)) "true" }}
    {{- $value = include "common.getNestedValue" (dict "Values" $values "key" $defaultKey) }}
  {{- else -}}
    {{- $value = "" -}}
  {{- end -}}
  {{- $value -}}
{{- end }}


{{- define "common.hasNestedKey" -}}
{{- /*
This function checks recursively if a nested key exists within a map.
Usage: {{ include "common.hasNestedKey" (dict "Values" .Values "key" "key1.key2.key3") }}
Returns: true or false (boolean).
*/ -}}
  {{- $map := .Values -}}
  {{- $keyPath := splitList "." .key -}}
  {{- $output := false -}}

  {{- if not (kindIs "map" $map) }}
    {{- $output = false -}}
  {{- else if eq (len $keyPath) 1 }}
    {{- $output = hasKey $map (first $keyPath) -}}
  {{- else }}
    {{- $currentKey := first $keyPath -}}
    {{- $remainingKeys := rest $keyPath | join "." -}}
    {{- $nextMap := get $map $currentKey -}}
    {{- if kindIs "map" $nextMap }}
      {{- $output = include "common.hasNestedKey" (dict "Values" $nextMap "key" $remainingKeys) -}}
    {{- else }}
      {{- $output = false -}}
    {{- end }}
  {{- end }}
  {{- $output -}}
{{- end }}



{{- define "common.getNestedValue" -}}
{{- /*
This function retrieves the value at a nested key within a map.
Usage: {{ include "common.getNestedValue" (dict "Values" .Values "key" "key1.key2.key3") }}
Returns: The value at the nested key path or "null" if the path does not exist.
*/ -}}
  {{- $map := .Values -}}
  {{- $keyPath := splitList "." .key -}}

  {{- if not (kindIs "map" $map) }}
    {{- fail "common.getNestedValue: Values must be a map" -}}
  {{- else if eq (len $keyPath) 1 }}
    {{- if hasKey $map (first $keyPath) }}
      {{- get $map (first $keyPath) -}}
    {{- else }}
      {{- "null" -}}
    {{- end }}
  {{- else }}
    {{- $currentKey := first $keyPath -}}
    {{- $remainingKeys := rest $keyPath | join "." -}}
    {{- if hasKey $map $currentKey }}
      {{- include "common.getNestedValue" (dict "Values" (get $map $currentKey) "key" $remainingKeys) -}}
    {{- else }}
      {{- "null" -}}
    {{- end }}
  {{- end }}
{{- end }}


