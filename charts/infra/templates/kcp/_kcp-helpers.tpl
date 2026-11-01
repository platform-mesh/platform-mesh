{{/*
Image reference as repository:tag, for CRD fields that take a single string
(etcd-druid's spec.etcd.image / spec.backup.image). Caller must guard on
.repository before including.
*/}}
{{- define "kcp.image.string" -}}
{{- .repository }}{{ if .tag }}:{{ .tag }}{{ end -}}
{{- end }}

{{/*
Image block as repository:/tag: keys, for CRD fields that take an ImageSpec
object (kcp-operator's RootShard/Shard/FrontProxy/CacheServer).
*/}}
{{- define "kcp.image.block" -}}
{{- if or .repository .tag -}}
image:
  {{- with .repository }}
  repository: {{ . }}
  {{- end }}
  {{- with .tag }}
  tag: {{ . }}
  {{- end }}
{{- end -}}
{{- end }}

{{- define "kcp.etcd" -}}
apiVersion: druid.gardener.cloud/v1alpha1
kind: Etcd
metadata:
  name: {{ .name }}
  namespace: {{ .namespace }}
  labels:
    app: etcd-statefulset
    gardener.cloud/role: controlplane
    role: kcp
spec:
  annotations:
    app: etcd-statefulset
    gardener.cloud/role: controlplane
    role: kcp
  labels:
    app: etcd-statefulset
    gardener.cloud/role: controlplane
    role: kcp
  etcd:
    metrics: basic
    {{- if .etcd.image.repository }}
    image: {{ include "kcp.image.string" .etcd.image }}  # image-schema:allow values come from OCM-injected repository/tag, not a literal
    {{- end }}
    defragmentationSchedule: {{ .etcd.defragmentationSchedule | default "\"0 */24 * * *\"" }}
    resources:
      limits:
        cpu: {{ .etcd.resources.limits.cpu | default "500m" }}
        memory: {{ .etcd.resources.limits.memory | default "1Gi" }}
      requests:
        cpu: {{ .etcd.resources.requests.cpu | default "100m" }}
        memory: {{ .etcd.resources.requests.memory | default "200Mi" }}
    clientPort: {{ .etcd.service.port | default 2379 }}
    serverPort: {{ .etcd.serverPort | default 2380 }}
    quota: {{ .etcd.quota | default "8Gi" }}
  backup:
    {{- if .etcd.backup.image.repository }}
    image: {{ include "kcp.image.string" .etcd.backup.image }}  # image-schema:allow values come from OCM-injected repository/tag, not a literal
    {{- end }}
    port: {{ .etcd.backup.port | default 8080 }}
    fullSnapshotSchedule: {{ .etcd.backup.fullSnapshotSchedule | default "\"0 */24 * * *\"" }}
    resources:
      limits:
        cpu: {{ .etcd.backup.resources.limits.cpu | default "200m" }}
        memory: {{ .etcd.backup.resources.limits.memory | default "1Gi" }}
      requests:
        cpu: {{ .etcd.backup.resources.requests.cpu | default "23m" }}
        memory: {{ .etcd.backup.resources.requests.memory | default "128Mi" }}
    garbageCollectionPolicy: {{ .etcd.backup.garbageCollectionPolicy | default "Exponential" }}
    garbageCollectionPeriod: {{ .etcd.backup.garbageCollectionPeriod | default "43200s" }}
    deltaSnapshotPeriod: {{ .etcd.backup.deltaSnapshotPeriod | default "300s" }}
    deltaSnapshotMemoryLimit: {{ .etcd.backup.deltaSnapshotMemoryLimit | default "1Gi" }}
    compression:
      enabled: {{ .etcd.backup.compression.enabled | default false }}
      policy: {{ .etcd.backup.compression.policy | default "\"gzip\"" }}
    leaderElection:
      reelectionPeriod: {{ .etcd.backup.leaderElection.reelectionPeriod | default "5s" }}
      etcdConnectionTimeout: {{ .etcd.backup.leaderElection.etcdConnectionTimeout | default "5s" }}
{{- if .etcd.backup.store }}
    store:
{{ toYaml .etcd.backup.store | indent 6 }}
{{- end }}

  sharedConfig:
    autoCompactionMode: {{ .etcd.sharedConfig.autoCompactionMode | default "periodic" }}
    autoCompactionRetention: {{ .etcd.sharedConfig.autoCompactionRetention | default "\"30m\"" }}


  replicas: {{ .etcd.replicas | default 1 }}
{{- end -}}
{{- define "kcp.tlsroute" -}}
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: TLSRoute
metadata:
  name: {{ .name }}
  namespace: {{ .namespace }}
spec:
  hostnames:
  - {{ .hostname }}
  parentRefs:
  - group: gateway.networking.k8s.io
    kind: Gateway
    name: {{ .gatewayName }}
    sectionName: {{ .sectionName }}
  rules:
  - backendRefs:
    - group: ""
      kind: Service
      name: {{ .serviceName }}
      namespace: {{ .namespace }}
      port: 6443
{{- end -}}
{{- define "kcp.shard.spec" -}}
replicas: {{ .replicas }}
shardBaseURL: {{ .shardBaseURL }}
proxy:
  deploymentTemplate:
    spec:
      template:
        spec:
          {{- include "common.hostAliases" .root | nindent 10 }}
  {{- with (include "kcp.image.block" .root.Values.kcp.image) }}{{ . | nindent 2 }}{{- end }}
{{- if or .root.Values.kcp.auth.serviceAccount.enabled .root.Values.kcp.auth.oidc.enabled }}
auth:
  {{- if .root.Values.kcp.auth.serviceAccount.enabled }}
  serviceAccount:
    enabled: true
  {{- end }}
  {{- if .root.Values.kcp.auth.oidc.enabled }}
  {{- with .root.Values.kcp.auth.oidc }}
  oidc:
    enabled: true
    issuerURL: {{ .issuerUrl }}
    caFileRef:
      name: {{ .caFileRef.name }}
      key: {{ .caFileRef.key }}
    clientID: {{ .clientID }}
    groupsClaim: {{ .groupsClaim }}
    usernameClaim: {{ .usernameClaim }}
  {{- end }}
  {{- end }}
{{- end }}
{{- if (.root.Values.kcp.webhook).enabled }}
authorization:
  webhook:
    configSecretName: {{ .root.Values.kcp.webhook.authorizationWebhookSecretName }}
    version: {{ .root.Values.kcp.webhook.version | default "v1" }}
{{- end }}
{{- with (include "kcp.image.block" .root.Values.kcp.image) }}
{{ . }}
{{- end }}
{{- if gt (len .resources) 0 }}
resources:
  {{- toYaml .resources | nindent 2 }}
{{- end }}
deploymentTemplate:
  spec:
    template:
      metadata:
        annotations:
          traffic.sidecar.istio.io/excludeOutboundPorts: "{{ .webhookPort }}"
      spec:
        {{- include "common.hostAliases" .root | nindent 8 }}
extraArgs:
{{ toYaml .extraArgs | indent 2 }}
{{- end -}}

{{- define "kcp.certificates" -}}
{{- $certs := .Values.kcp.certificates | default dict -}}
{{- $issuerRef := $certs.issuerRef | default dict -}}
{{- $caSecretRef := $certs.caSecretRef | default dict -}}
{{- if and $issuerRef $caSecretRef -}}
{{- fail "kcp.certificates.issuerRef and kcp.certificates.caSecretRef are mutually exclusive, set at most one" -}}
{{- end -}}
{{- if and $caSecretRef (not $caSecretRef.name) -}}
{{- fail "kcp.certificates.caSecretRef.name is required when caSecretRef is set" -}}
{{- end -}}
{{- if and $issuerRef (not $issuerRef.name) -}}
{{- fail "kcp.certificates.issuerRef.name is required when issuerRef is set" -}}
{{- end -}}
{{- if $caSecretRef -}}
caSecretRef:
  name: {{ $caSecretRef.name }}
{{- else if $issuerRef -}}
issuerRef:
  group: {{ $issuerRef.group | default "cert-manager.io" }}
  kind: {{ $issuerRef.kind | default "Issuer" }}
  name: {{ $issuerRef.name }}
{{- else -}}
issuerRef:
  group: cert-manager.io
  kind: Issuer
  name: selfsigned
{{- end -}}
{{- end -}}

{{- define "kcp.certificates.createIssuer" -}}
{{- $certs := .Values.kcp.certificates | default dict -}}
{{- if not (or ($certs.issuerRef | default dict) ($certs.caSecretRef | default dict)) -}}
true
{{- end -}}
{{- end -}}
{{- /* Name of the passthrough listener a kcp TLSRoute attaches to. */ -}}
{{- define "kcp.passthroughSectionName" -}}
{{- if and .root.Values.gatewayApi.passthrough.enabled .suffix -}}
passthrough-{{ .suffix }}
{{- else -}}
passthrough
{{- end -}}
{{- end -}}
{{- /* One passthrough listener per kcp hostname; Istio rejects same-port listeners that share an empty hostname. */ -}}
{{- define "kcp.passthroughListeners" -}}
{{- $pt := .Values.gatewayApi.passthrough -}}
{{- $external := .Values.kcp.external.hostname -}}
{{- $hosts := list (dict "suffix" "" "hostname" $external) -}}
{{- $hosts = append $hosts (dict "suffix" "root" "hostname" (.Values.kcp.rootShard.hostname | default (printf "root.kcp.%s" $external))) -}}
{{- range .Values.kcp.shards -}}
{{- $hosts = append $hosts (dict "suffix" .name "hostname" (.hostname | default (printf "%s.kcp.%s" .name $external))) -}}
{{- end -}}
{{- range $hosts }}
- name: {{ include "kcp.passthroughSectionName" (dict "root" $ "suffix" .suffix) }}
  hostname: {{ .hostname | quote }}
  port: {{ $pt.port }}
  protocol: TLS
  tls:
    mode: Passthrough
  {{- with $pt.allowedRoutes }}
  allowedRoutes:
    {{- toYaml . | nindent 4 }}
  {{- end }}
{{- end }}
{{- end -}}
