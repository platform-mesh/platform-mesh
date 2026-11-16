{{- define "infra.credentials.name" -}}
{{- printf "%s-credentials-bootstrap" (.Release.Name | trunc 40 | trimSuffix "-") -}}
{{- end -}}

{{- define "infra.credentials.hooks" -}}
helm.sh/hook: pre-install,pre-upgrade
helm.sh/hook-weight: "-10"
helm.sh/hook-delete-policy: before-hook-creation
argocd.argoproj.io/hook: PreSync
argocd.argoproj.io/sync-wave: "-10"
argocd.argoproj.io/hook-delete-policy: BeforeHookCreation
{{- end -}}

{{- define "infra.credentials.config" -}}
{{- $groups := list -}}
{{- if .Values.cnpg.enabled -}}
  {{- $db := list (dict "name" "cnpg-keycloak-user" "username" .Values.cnpg.roles.keycloak.username "password" .Values.cnpg.roles.keycloak.password) -}}
  {{- if and .Values.keycloak.operator.enabled (eq (include "infra.keycloak.sharedDatabase" .) "true") -}}
    {{- $legacy := default "keycloak-db-credentials" .Values.keycloak.operator.db.existingSecret -}}
    {{- if ne $legacy "cnpg-keycloak-user" -}}
      {{- $spec := dict "name" $legacy "username" .Values.keycloak.operator.db.username "password" .Values.keycloak.operator.db.password "readOnly" true -}}
      {{- if .Values.keycloak.operator.db.existingSecret -}}{{- $_ := set $spec "required" true -}}{{- end -}}
      {{- $db = append $db $spec -}}
    {{- else -}}
      {{- $_ := set (index $db 0) "required" true -}}
    {{- end -}}
  {{- end -}}
  {{- $groups = append $groups $db -}}
  {{- $groups = append $groups (list (dict "name" "cnpg-openfga-user" "username" .Values.cnpg.roles.openfga.username "password" .Values.cnpg.roles.openfga.password)) -}}
{{- end -}}
{{- dict "groups" $groups | toJson -}}
{{- end -}}

{{- define "infra.keycloak.sharedDatabase" -}}
{{- $host := .Values.keycloak.operator.db.host -}}
{{- $cnpgHost := printf "%s-rw" .Values.cnpg.cluster.name -}}
{{- $localNames := list $cnpgHost (printf "%s.%s" $cnpgHost .Release.Namespace) (printf "%s.%s.svc" $cnpgHost .Release.Namespace) -}}
{{- and .Values.cnpg.enabled (or (has $host $localNames) (hasPrefix (printf "%s.%s.svc." $cnpgHost .Release.Namespace) $host)) -}}
{{- end -}}

{{- define "infra.keycloak.adminSecret" -}}
{{- if .Values.keycloak.operator.admin.existingSecret -}}
  {{- if .Values.keycloak.operator.admin.password -}}{{- fail "admin.existingSecret and admin.password cannot be used together" -}}{{- end -}}
  {{- .Values.keycloak.operator.admin.existingSecret -}}
{{- else if .Values.keycloak.operator.admin.password -}}
  keycloak-admin
{{- else -}}
  keycloak-initial-admin
{{- end -}}
{{- end -}}

{{- define "infra.keycloak.databaseSecret" -}}
{{- if .Values.keycloak.operator.db.existingSecret -}}
  {{- if .Values.keycloak.operator.db.password -}}{{- fail "db.existingSecret and db.password cannot be used together" -}}{{- end -}}
  {{- .Values.keycloak.operator.db.existingSecret -}}
{{- else if .Values.keycloak.operator.db.password -}}
  keycloak-db-credentials
{{- else if eq (include "infra.keycloak.sharedDatabase" .) "true" -}}
  cnpg-keycloak-user
{{- else -}}
  {{- fail "keycloak.operator.db.existingSecret or db.password is required for an external database" -}}
{{- end -}}
{{- end -}}
