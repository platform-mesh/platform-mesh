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
