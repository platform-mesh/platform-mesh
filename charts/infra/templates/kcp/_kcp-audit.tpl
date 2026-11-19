{{- /* kcp audit helpers. All take the root context unless noted. */ -}}

{{- /* Audit policy document: kcp.audit.policy.inline if set, else the bundled default. */ -}}
{{- define "kcp.audit.policyContent" -}}
{{- $inline := ((.Values.kcp.audit).policy).inline | default dict -}}
{{- if $inline -}}
{{- toYaml $inline -}}
{{- else -}}
{{- .Files.Get "files/kcp-audit-policy.yaml" -}}
{{- end -}}
{{- end -}}

{{- /* Reference (name/key) to the ConfigMap holding the audit policy. */ -}}
{{- define "kcp.audit.configMapRef" -}}
{{- $existing := ((.Values.kcp.audit).policy).existingConfigMap | default dict -}}
{{- if $existing -}}
name: {{ $existing.name }}
key: {{ $existing.key }}
{{- else -}}
name: kcp-audit-policy
key: policy.yaml
{{- end -}}
{{- end -}}

{{- /* "true" when audit is enabled globally or on any shard, else empty. */ -}}
{{- define "kcp.audit.anyEnabled" -}}
{{- $enabled := (.Values.kcp.audit).enabled -}}
{{- range .Values.kcp.shards -}}
{{- if ((.spec).audit).enabled -}}
{{- $enabled = true -}}
{{- end -}}
{{- end -}}
{{- if $enabled -}}true{{- end -}}
{{- end -}}

{{- /*
Validate an audit config. Args: dict "audit" <map> "path" <values path for messages>.
Only checks when audit.enabled is true.
*/ -}}
{{- define "kcp.audit.validate" -}}
{{- $a := .audit | default dict -}}
{{- $p := .path -}}
{{- if $a.enabled -}}
{{- $log := $a.log | default dict -}}
{{- $wh := $a.webhook | default dict -}}
{{- if not (or $log.enabled $wh.enabled) -}}
{{- fail (printf "%s.enabled requires at least one backend: set %s.log.enabled or %s.webhook.enabled" $p $p $p) -}}
{{- end -}}
{{- if and $wh.enabled (not $wh.configSecretName) -}}
{{- fail (printf "%s.webhook.configSecretName is required when %s.webhook.enabled is true" $p $p) -}}
{{- end -}}
{{- if and $log.enabled (not (has ($log.format | default "json") (list "json" "legacy"))) -}}
{{- fail (printf "%s.log.format must be \"json\" or \"legacy\"" $p) -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- /*
Render the spec.audit block of a RootShard/Shard, or nothing when audit is disabled.
Args: dict "root" $ "audit" <map> "path" <values path for messages>.
*/ -}}
{{- define "kcp.audit.spec" -}}
{{- include "kcp.audit.validate" . -}}
{{- $a := .audit | default dict -}}
{{- if $a.enabled -}}
audit:
  policy:
    configMap:
      {{- include "kcp.audit.configMapRef" .root | nindent 6 }}
  {{- $wh := $a.webhook | default dict }}
  {{- if $wh.enabled }}
  {{- /* omit returns a copy, so the coercion below never touches .Values. */}}
  {{- $webhook := omit $wh "enabled" }}
  {{- /* The CRD types batchThrottleQPS as a string; values files may give a number. */}}
  {{- if hasKey $webhook "batchThrottleQPS" }}
  {{- $_ := set $webhook "batchThrottleQPS" (toString $webhook.batchThrottleQPS) }}
  {{- end }}
  webhook:
    {{- toYaml $webhook | nindent 4 }}
  {{- end }}
{{- end -}}
{{- end -}}
