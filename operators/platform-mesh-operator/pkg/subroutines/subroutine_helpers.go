/*
Copyright The Platform Mesh Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package subroutines

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/template"
	"time"

	certmanager "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	fluxcdv2 "github.com/fluxcd/helm-controller/api/v2"
	fluxcdv1 "github.com/fluxcd/source-controller/api/v1beta2"
	"github.com/rs/zerolog/log"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmprovidersv1alpha1 "go.platform-mesh.io/apis/providers/v1alpha1"
	pmconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/platform-mesh-operator/internal/config"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	kcpapiv1alpha "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"
	kcpapiv1alpha2 "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha2"
	kcpcorev1alpha "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
)

type KcpHelper interface {
	NewKcpClient(config *rest.Config, workspacePath string) (ctrlruntimeclient.Client, error)
}

type Helper struct {
}

func (h *Helper) NewKcpClient(config *rest.Config, workspacePath string) (ctrlruntimeclient.Client, error) {
	config.QPS = 1000.0
	config.Burst = 2000.0
	u, err := url.Parse(config.Host)
	if err != nil {
		return nil, errors.Wrap(err, "Unable to parse kcp host: %s", config.Host)
	}
	config.Host = u.Scheme + "://" + u.Host + "/clusters/" + workspacePath
	scheme := runtime.NewScheme()
	utilruntime.Must(appsv1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(authenticationv1.AddToScheme(scheme))
	utilruntime.Must(pmcorev1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcpapiv1alpha.AddToScheme(scheme))
	utilruntime.Must(kcpapiv1alpha2.AddToScheme(scheme))
	utilruntime.Must(kcptenancyv1alpha.AddToScheme(scheme))
	utilruntime.Must(kcpcorev1alpha.AddToScheme(scheme))
	utilruntime.Must(rbacv1.AddToScheme(scheme))
	utilruntime.Must(admissionregistrationv1.AddToScheme(scheme))
	utilruntime.Must(pmprovidersv1alpha1.AddToScheme(scheme))

	cl, err := ctrlruntimeclient.New(config, ctrlruntimeclient.Options{
		Scheme: scheme,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to create KCP client: %w", err)
	}
	return cl, nil
}

func GetSecret(client ctrlruntimeclient.Client, name string, namespace string) (*corev1.Secret, error) {
	secret := corev1.Secret{}
	err := client.Get(context.Background(), types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}, &secret)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get secret")
	}
	return &secret, nil
}

// AppendRootShardCAPEMIfMissing loads {RootShardName}-ca tls.crt and appends it to caData when the root cert is not already in the bundle.
func AppendRootShardCAPEMIfMissing(ctx context.Context, k8sClient ctrlruntimeclient.Client, operatorCfg *config.OperatorConfig, caData []byte) []byte {
	log := logger.LoadLoggerFromContext(ctx)
	if len(caData) == 0 {
		log.Debug().Msg("Skip appending root-shard CA: empty CA data")
		return caData
	}
	if operatorCfg == nil {
		log.Debug().Msg("Skip appending root-shard CA: operator config is nil")
		return caData
	}
	if operatorCfg.KCP.RootShardName == "" || operatorCfg.KCP.Namespace == "" {
		log.Debug().
			Str("rootShardName", operatorCfg.KCP.RootShardName).
			Str("kcpNamespace", operatorCfg.KCP.Namespace).
			Msg("Skip appending root-shard CA: empty KCP root shard name or namespace")
		return caData
	}
	secretName := operatorCfg.KCP.RootShardName + "-ca"
	ns := operatorCfg.KCP.Namespace
	rootSecret, rootErr := GetSecret(k8sClient, secretName, ns)
	if rootErr != nil {
		if apierrors.IsNotFound(rootErr) {
			log.Debug().
				Str("secret", secretName).
				Str("namespace", ns).
				Msg("Root-shard CA secret not found, leaving CA bundle unchanged")
		} else {
			log.Warn().Err(rootErr).
				Str("secret", secretName).
				Str("namespace", ns).
				Msg("Failed to get root-shard CA secret, leaving CA bundle unchanged")
		}
		return caData
	}
	if rootSecret == nil {
		log.Debug().Str("secret", secretName).Str("namespace", ns).Msg("Root-shard CA secret is nil, leaving CA bundle unchanged")
		return caData
	}
	rootPEM, ok := rootSecret.Data["tls.crt"]
	if !ok || len(rootPEM) == 0 {
		log.Debug().
			Str("secret", secretName).
			Str("namespace", ns).
			Msg("Root-shard CA secret has no tls.crt, leaving CA bundle unchanged")
		return caData
	}
	merged, outcome, mergeErr := mergeRootCAPEMIfMissing(caData, rootPEM)
	switch outcome {
	case mergeRootCAAppended:
		log.Info().
			Str("secret", secretName).
			Str("namespace", ns).
			Msg("Appended root-shard CA to PEM bundle")
	case mergeRootCAUnchangedAlreadyPresent:
		log.Debug().
			Str("secret", secretName).
			Str("namespace", ns).
			Msg("Root-shard CA already present in PEM bundle, not appended")
	case mergeRootCAUnchangedInvalidRootPEM:
		log.Warn().Err(mergeErr).
			Str("secret", secretName).
			Str("namespace", ns).
			Msg("Root-shard CA tls.crt is not valid PEM certificate data, not appended")
	}
	return merged
}

func firstCertificateFromPEM(pemData []byte) (*x509.Certificate, error) {
	for {
		var block *pem.Block
		block, pemData = pem.Decode(pemData)
		if block == nil {
			return nil, fmt.Errorf("no PEM certificate found")
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		return x509.ParseCertificate(block.Bytes)
	}
}

func pemBundleContainsCertificate(pemData []byte, want *x509.Certificate) bool {
	if want == nil {
		return false
	}
	for {
		var block *pem.Block
		block, pemData = pem.Decode(pemData)
		if block == nil {
			return false
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		if bytes.Equal(block.Bytes, want.Raw) {
			return true
		}
	}
}

// mergeRootCAOutcome describes why mergeRootCAPEMIfMissing did or did not change caData.
type mergeRootCAOutcome int

const (
	mergeRootCAUnchangedNoMerge mergeRootCAOutcome = iota // empty caData or rootPEM (caller typically skips logging)
	mergeRootCAUnchangedInvalidRootPEM
	mergeRootCAUnchangedAlreadyPresent
	mergeRootCAAppended
)

// mergeRootCAPEMIfMissing appends rootPEM to caData when the root cert is not already in the bundle.
// mergeErr is set only when outcome is mergeRootCAUnchangedInvalidRootPEM.
func mergeRootCAPEMIfMissing(caData, rootPEM []byte) (merged []byte, outcome mergeRootCAOutcome, mergeErr error) {
	if len(caData) == 0 || len(rootPEM) == 0 {
		return caData, mergeRootCAUnchangedNoMerge, nil
	}
	rootCert, err := firstCertificateFromPEM(rootPEM)
	if err != nil {
		return caData, mergeRootCAUnchangedInvalidRootPEM, err
	}
	if pemBundleContainsCertificate(caData, rootCert) {
		return caData, mergeRootCAUnchangedAlreadyPresent, nil
	}
	return append(append(append([]byte(nil), caData...), '\n'), rootPEM...), mergeRootCAAppended, nil
}

// appendPEMCertsDedupe appends PEM CERTIFICATE blocks from extra to bundle when the cert is not already present.
func appendPEMCertsDedupe(bundle, extra []byte) []byte {
	rest := extra
	for len(rest) > 0 {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		if pemBundleContainsCertificate(bundle, cert) {
			continue
		}
		bundle = append(bundle, pem.EncodeToMemory(block)...)
	}
	return bundle
}

func ReplaceTemplate(templateData map[string]any, templateBytes []byte) ([]byte, error) {
	funcMap := template.FuncMap{
		"indent": func(spaces int, s string) string {
			pad := strings.Repeat(" ", spaces)
			lines := strings.Split(s, "\n")
			for i, line := range lines {
				if line != "" {
					lines[i] = pad + line
				}
			}
			return strings.Join(lines, "\n")
		},
	}

	tmpl, err := template.New("manifest").Funcs(funcMap).Parse(string(templateBytes))
	if err != nil {
		return []byte{}, errors.Wrap(err, "Failed to parse template")
	}
	var result bytes.Buffer
	err = tmpl.Execute(&result, templateData)
	if err != nil {
		keys := make([]string, 0, len(templateData))
		for k := range templateData {
			keys = append(keys, k)
		}
		return []byte{}, errors.Wrap(err, "Failed to execute template with keys %v", keys)
	}
	if result.Len() == 0 {
		return []byte{}, nil
	}
	return result.Bytes(), nil
}

func ConvertToUnstructured(webhook admissionregistrationv1.MutatingWebhookConfiguration) (*unstructured.Unstructured, error) {
	// Convert the structured object to a map
	objMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&webhook)
	if err != nil {
		return nil, err
	}
	// Create an unstructured object and assign the map
	unstructuredObj := &unstructured.Unstructured{Object: objMap}
	unstructuredObj.SetKind("MutatingWebhookConfiguration")
	unstructuredObj.SetAPIVersion("admissionregistration.k8s.io/v1")
	unstructuredObj.SetManagedFields(nil)
	return unstructuredObj, nil
}

func GetWorkspaceDirs(dir string) []string {
	workspaces := []string{}
	// find all subdirectories named "dd-name", e.g. "01-platform-mesh-system"
	dirs, err := os.ReadDir(dir)
	if err != nil {
		// TODO: print error
		return workspaces
	}
	for _, d := range dirs {
		// check if d.Name() match the regex ^[0-9]{2}-[a-zA-Z0-9-]+$
		if d.IsDir() {
			if IsWorkspace(d.Name()) {
				workspaces = append(workspaces, d.Name())
			}
		}
	}
	return workspaces
}

func GetWorkspaceName(dir string) (string, error) {
	validWorkspaceName := regexp.MustCompile(`.*[0-9]{2}-([a-zA-Z0-9-]+)$`)
	matches := validWorkspaceName.FindAllSubmatch([]byte(dir), -1)
	if matches == nil {
		return "", fmt.Errorf("invalid workspace name: %s", dir)
	}
	last := matches[len(matches)-1]
	return string(last[1]), nil
}

func IsWorkspace(dir string) bool {
	pattern := `^[0-9]{2}-[a-zA-Z0-9-]+$`
	match, err := regexp.Match(pattern, []byte(dir))
	if err != nil {
		return false
	}
	return match
}

func ListFiles(dir string) ([]string, error) {
	files := []string{}
	// find all files in the directory
	dirs, err := os.ReadDir(dir)
	if err != nil {
		return files, errors.Wrap(err, "Failed to read directory")
	}
	for _, d := range dirs {
		if d.IsDir() {
			continue
		}
		files = append(files, d.Name())
	}
	sort.Strings(files)
	return files, nil
}

func MergeValuesAndServices(inst *pmcorev1alpha1.PlatformMesh, templateVars apiextensionsv1.JSON, config config.OperatorConfig) (apiextensionsv1.JSON, error) {
	services := inst.Spec.Values
	var mapValues map[string]any
	if len(templateVars.Raw) > 0 {
		if err := json.Unmarshal(templateVars.Raw, &mapValues); err != nil {
			return apiextensionsv1.JSON{}, err
		}
	} else {
		mapValues = map[string]any{}
	}
	// Unmarshal 'services'
	var mapServices map[string]any
	if len(services.Raw) > 0 {
		if err := json.Unmarshal(services.Raw, &mapServices); err != nil {
			return apiextensionsv1.JSON{}, err
		}
	} else {
		mapServices = map[string]any{}
	}

	// Create 'services' key in 'values' if it doesn't exist
	if _, ok := mapValues["services"]; !ok {
		mapValues["services"] = map[string]any{}
	}

	// add 'services' to mapValues["services"]
	if _, ok := mapValues["services"].(map[string]any); !ok {
		return apiextensionsv1.JSON{}, fmt.Errorf("services is not a map")
	}
	for k, v := range mapServices {
		mapValues["services"].(map[string]any)[k] = v
	}

	mergeOCMConfig(mapValues, inst)

	mapValues["kubeConfigEnabled"] = config.RemoteRuntime.IsEnabled()
	if config.RemoteRuntime.IsEnabled() {
		mapValues["kubeConfigSecretName"] = config.RemoteRuntime.InfraSecretName
		mapValues["kubeConfigSecretKey"] = config.RemoteRuntime.InfraSecretKey
	}

	// Marshal back to apiextensionsv1.JSON
	mergedRaw, err := json.Marshal(mapValues)
	if err != nil {
		return apiextensionsv1.JSON{}, err
	}
	return apiextensionsv1.JSON{Raw: mergedRaw}, nil
}

type exposureParams struct {
	baseDomain             string
	baseDomainPort         string
	port                   int
	protocol               string
	traefikClusterIP       string
	kcpFrontProxyClusterIP string
}

func getExposureParams(inst *pmcorev1alpha1.PlatformMesh) exposureParams {
	p := exposureParams{
		port:                   8443,
		baseDomain:             "portal.localhost",
		protocol:               "https",
		traefikClusterIP:       "10.96.188.4",
		kcpFrontProxyClusterIP: "10.96.0.100",
	}

	if inst.Spec.Exposure != nil {
		if inst.Spec.Exposure.Port != 0 {
			p.port = inst.Spec.Exposure.Port
		}
		if inst.Spec.Exposure.BaseDomain != "" {
			p.baseDomain = inst.Spec.Exposure.BaseDomain
		}
		if inst.Spec.Exposure.Protocol != "" {
			p.protocol = inst.Spec.Exposure.Protocol
		}
		if inst.Spec.Exposure.TraefikClusterIP != "" {
			p.traefikClusterIP = inst.Spec.Exposure.TraefikClusterIP
		}
		if inst.Spec.Exposure.KcpFrontProxyClusterIP != "" {
			p.kcpFrontProxyClusterIP = inst.Spec.Exposure.KcpFrontProxyClusterIP
		}
	}

	if p.port == 80 || p.port == 443 {
		p.baseDomainPort = p.baseDomain
	} else {
		p.baseDomainPort = fmt.Sprintf("%s:%d", p.baseDomain, p.port)
	}
	return p
}

func (p exposureParams) portString() string {
	return fmt.Sprintf("%d", p.port)
}

func (p exposureParams) baseDomainWithPort() string {
	if p.port == 443 {
		return p.baseDomain
	}
	return fmt.Sprintf("%s:%d", p.baseDomain, p.port)
}

func (p exposureParams) internalFrontProxyURL(kcp config.KCPConfig) string {
	return fmt.Sprintf("https://%s-front-proxy.%s:%s", kcp.FrontProxyName, kcp.Namespace, kcp.FrontProxyPort)
}

func (p exposureParams) templateVars(kcp config.KCPConfig) map[string]any {
	return map[string]any{
		"baseDomain":             p.baseDomain,
		"baseDomainPort":         p.baseDomainPort,
		"port":                   p.portString(),
		"protocol":               p.protocol,
		"traefikClusterIP":       p.traefikClusterIP,
		"kcpFrontProxyClusterIP": p.kcpFrontProxyClusterIP,
		"internalFrontProxyUrl":  p.internalFrontProxyURL(kcp),
	}
}

func TemplateVars(ctx context.Context, inst *pmcorev1alpha1.PlatformMesh, cl ctrlruntimeclient.Client) (apiextensionsv1.JSON, error) {
	operatorCfg := pmconfig.LoadConfigFromContext(ctx).(config.OperatorConfig)

	values := getExposureParams(inst).templateVars(operatorCfg.KCP)
	values["helmReleaseNamespace"] = inst.Namespace

	result := apiextensionsv1.JSON{}
	result.Raw, _ = json.Marshal(values)
	raw, err := json.Marshal(values)
	if err != nil {
		return apiextensionsv1.JSON{}, errors.Wrap(err, "Failed to marshal template vars")
	}
	result.Raw = raw

	return result, nil
}

func buildKubeconfig(ctx context.Context, client ctrlruntimeclient.Client, kcpUrl string) (*rest.Config, error) {
	operatorCfg := pmconfig.LoadConfigFromContext(ctx).(config.OperatorConfig)
	return BuildKubeconfigFromConfig(client, &operatorCfg.KCP, kcpUrl)
}

// BuildKubeconfigFromConfig builds a *rest.Config for the kcp admin from the cluster-admin
// certificate Secret. It is the exported equivalent of buildKubeconfigFromConfig.
func BuildKubeconfigFromConfig(client ctrlruntimeclient.Client, kcpConfig *config.KCPConfig, kcpUrl string) (*rest.Config, error) {
	secretName := kcpConfig.ClusterAdminSecretName
	secret, err := GetSecret(client, secretName, kcpConfig.Namespace)
	if err != nil {
		return nil, fmt.Errorf("getting secret %s/%s: %w", kcpConfig.Namespace, secretName, err)
	}
	if secret == nil {
		return nil, fmt.Errorf("secret %s/%s is nil", kcpConfig.Namespace, secretName)
	}
	if secret.Data == nil {
		return nil, fmt.Errorf("secret %s/%s has no Data", kcpConfig.Namespace, secretName)
	}

	// Try kubeconfig key first (Opaque secret with pre-built kubeconfig)
	if kubeconfigData, ok := secret.Data["kubeconfig"]; ok && len(kubeconfigData) > 0 {
		cfg, err := clientcmd.Load(kubeconfigData)
		if err != nil {
			return nil, fmt.Errorf("failed to parse kubeconfig from secret %s/%s: %w", kcpConfig.Namespace, secretName, err)
		}
		// Override the server URL in all clusters with the provided kcpUrl
		for _, cluster := range cfg.Clusters {
			cluster.Server = kcpUrl
		}
		return clientcmd.NewDefaultClientConfig(*cfg, nil).ClientConfig()
	}

	// Fall back to cert-based approach (kubernetes.io/tls secret with ca.crt, tls.crt, tls.key)
	caData, ok := secret.Data["ca.crt"]
	if !ok || len(caData) == 0 {
		return nil, fmt.Errorf("secret %s/%s missing both \"kubeconfig\" and \"ca.crt\" keys", kcpConfig.Namespace, secretName)
	}
	tlsCrt, ok := secret.Data["tls.crt"]
	if !ok || len(tlsCrt) == 0 {
		return nil, fmt.Errorf("secret %s/%s missing or empty key \"tls.crt\"", kcpConfig.Namespace, secretName)
	}
	tlsKey, ok := secret.Data["tls.key"]
	if !ok || len(tlsKey) == 0 {
		return nil, fmt.Errorf("secret %s/%s missing or empty key \"tls.key\"", kcpConfig.Namespace, secretName)
	}

	cfg := clientcmdapi.NewConfig()
	cfg.Clusters = map[string]*clientcmdapi.Cluster{
		"kcp": {
			Server:                   kcpUrl,
			CertificateAuthorityData: caData,
		},
	}
	cfg.Contexts = map[string]*clientcmdapi.Context{
		"admin": {
			Cluster:  "kcp",
			AuthInfo: "admin",
		},
	}
	cfg.AuthInfos = map[string]*clientcmdapi.AuthInfo{
		"admin": {
			ClientCertificateData: tlsCrt,
			ClientKeyData:         tlsKey,
		},
	}
	cfg.CurrentContext = "admin"
	return clientcmd.NewDefaultClientConfig(*cfg, nil).ClientConfig()
}

func WaitForWorkspace(
	ctx context.Context,
	config *rest.Config, name string, log *logger.Logger,
	kcpHelper KcpHelper,
) error {
	client, err := kcpHelper.NewKcpClient(config, "root")
	if err != nil {
		return err
	}

	err = wait.PollUntilContextTimeout(
		ctx, time.Second, time.Second*15, true,
		func(ctx context.Context) (bool, error) {
			ws := &kcptenancyv1alpha.Workspace{}
			if err := client.Get(ctx, types.NamespacedName{Name: name}, ws); err != nil {
				return false, nil //nolint:nilerr
			}
			ready := ws.Status.Phase == "Ready"
			log.Info().Str("workspace", name).Bool("ready", ready).Msg("waiting for workspace to be ready")
			return ready, nil
		})

	if err != nil {
		return fmt.Errorf("workspace %s did not become ready: %w", name, err)
	}
	return err
}

func ApplyManifestFromFile(
	ctx context.Context,
	path string, k8sClient ctrlruntimeclient.Client, templateData map[string]any, wsPath string, inst *pmcorev1alpha1.PlatformMesh,
) error {
	log := logger.LoadLoggerFromContext(ctx)

	obj, err := unstructuredFromFile(path, templateData, log)
	if err != nil {
		return err
	}
	if obj.Object == nil {
		return nil
	}

	if obj.GetKind() == "ContentConfiguration" && obj.GetAPIVersion() == "ui.platform-mesh.io/v1alpha1" {
		if templateData["featureDisableContentConfigurations"] == "true" {
			log.Debug().Str("file", path).Str("kind", obj.GetKind()).Str("name", obj.GetName()).
				Msg("Skipping ContentConfiguration due to feature-disable-contentconfigurations toggle")
			return nil
		}
	}

	if obj.GetKind() == "WorkspaceType" && obj.GetAPIVersion() == "tenancy.kcp.io/v1alpha1" {
		extraDefaultApiBindings := getExtraDefaultApiBindings(obj, wsPath, inst)
		currentDefAPiBindings, found, err := unstructured.NestedSlice(obj.Object, "spec", "defaultAPIBindings")
		if err != nil || !found {
			currentDefAPiBindings = []any{}
		}
		for _, v := range extraDefaultApiBindings {
			newExport := kcptenancyv1alpha.APIExportReference{Path: v.Path, Export: v.Export}
			var m map[string]any
			b, marshalErr := yaml.Marshal(newExport)
			if marshalErr != nil {
				return errors.Wrap(marshalErr, "Failed to marshal APIExportReference")
			}
			if unmarshalErr := yaml.Unmarshal(b, &m); unmarshalErr != nil {
				return errors.Wrap(unmarshalErr, "Failed to unmarshal APIExportReference")
			}
			currentDefAPiBindings = append(currentDefAPiBindings, m)
		}
		err = unstructured.SetNestedSlice(obj.Object, currentDefAPiBindings, "spec", "defaultAPIBindings")
		if err != nil {
			return errors.Wrap(err, "Failed to set defaultAPIBindings")
		}
	}

	if (obj.GetKind() == "APIExport" || obj.GetKind() == "APIBinding") && obj.GetName() == "core.platform-mesh.io" {
		apiExport := kcpapiv1alpha.APIExport{}
		err = k8sClient.Get(ctx, types.NamespacedName{Name: "system.platform-mesh.io"}, &apiExport)
		if err != nil {
			return errors.Wrap(err, "Failed to get APIExport system.platform-mesh.io")
		}

		templateData["apiExportSystemPlatformMeshIoIdentityHash"] = apiExport.Status.IdentityHash
	}

	if obj.GetKind() == "APIExport" && obj.GetName() == "org-idp.platform-mesh.io" {
		rerendered, fillErr := fillOrgIdpCoreIdentityHash(ctx, k8sClient, path, templateData, log)
		if fillErr != nil {
			return fillErr
		}
		obj = rerendered
	}

	err = k8sClient.Apply(ctx, ctrlruntimeclient.ApplyConfigurationFromUnstructured(&obj),
		ctrlruntimeclient.FieldOwner("platform-mesh-operator"), ctrlruntimeclient.ForceOwnership)
	if err != nil {
		if obj.GetKind() == "IdentityProviderConfiguration" && obj.GetAPIVersion() == "core.platform-mesh.io/v1alpha1" {
			log.Warn().Err(err).Str("file", path).Str("kind", obj.GetKind()).Str("name", obj.GetName()).
				Msg("Failed to apply IdentityProviderConfiguration (webhook may not be ready yet), will retry on next reconciliation")
			return nil
		}
		return errors.Wrap(err, "Failed to apply manifest file: %s (%s/%s)", path, obj.GetKind(), obj.GetName())
	}
	log.Info().Str("file", path).Str("kind", obj.GetKind()).Str("name", obj.GetName()).Msg("Applied manifest file")
	return nil
}

func ApplyDirStructure(
	ctx context.Context,
	dir string,
	kcpPath string,
	config *rest.Config,
	templateData map[string]any,
	inst *pmcorev1alpha1.PlatformMesh,
	kcpHelper KcpHelper,
) error {
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("subroutine", "")

	k8sClient, err := kcpHelper.NewKcpClient(config, kcpPath)
	if err != nil {
		return err
	}

	// apply all manifest files in the current directory first
	files, err := ListFiles(dir)
	if err != nil {
		return errors.Wrap(err, "Failed to list files in workspace")
	}
	var errApplyManifests error
	for _, file := range files {
		log.Debug().Str("file", file).Msg("Applying file")
		path := filepath.Join(dir, file)
		err := ApplyManifestFromFile(ctx, path, k8sClient, templateData, kcpPath, inst)
		if err != nil {
			log.Warn().Err(err).Str("file", path).Msg("Failed to apply manifest file, continuing to next file in directory")
			errApplyManifests = err
		}
	}
	if errApplyManifests != nil {
		return errApplyManifests
	}

	for _, wsDir := range GetWorkspaceDirs(dir) {
		wsName, err := GetWorkspaceName(wsDir)
		if err != nil {
			log.Warn().Err(err).Str("Directory", dir).Str("wsName", wsName).Msg("Failed to get workspace path, skipping")
			continue
		}
		wsPath := fmt.Sprintf("%s:%s", kcpPath, wsName)
		if wsName == kcpPath {
			// the directory targets the current workspace itself (e.g. "02-root"
			// while already at "root"), so there is no child workspace to wait for.
			wsPath = kcpPath
		} else {
			err = WaitForWorkspace(ctx, config, wsName, log, kcpHelper)
			if err != nil {
				return err
			}
		}

		err = ApplyDirStructure(ctx, dir+"/"+wsDir, wsPath, config, templateData, inst, kcpHelper)
		if err != nil {
			return err
		}
	}

	return nil
}

func matchesConditionWithStatus(resource *unstructured.Unstructured, conditionType string, conditionStatus string) bool {
	if resource == nil {
		return false
	}
	conditions, found, err := unstructured.NestedSlice(resource.Object, "status", "conditions")
	if err != nil || !found {
		return false
	}

	for _, condition := range conditions {
		c, ok := condition.(map[string]any)
		if !ok {
			continue
		}
		if c["type"] == conditionType && c["status"] == conditionStatus {
			return true
		}
	}

	return false
}

const (
	orgIdpAPIExportName                        = "org-idp.platform-mesh.io"
	coreAPIExportName                          = "core.platform-mesh.io"
	apiExportCorePlatformMeshIoIdentityHashKey = "apiExportCorePlatformMeshIoIdentityHash"
)

func isPlaceholderIdentityHash(v string) bool {
	switch strings.TrimSpace(v) {
	case "", "<no value>":
		return true
	default:
		return false
	}
}

func accountInfoClaimIdentityHash(obj unstructured.Unstructured) (string, bool) {
	claims, found, err := unstructured.NestedSlice(obj.Object, "spec", "permissionClaims")
	if err != nil || !found {
		return "", false
	}
	for _, claim := range claims {
		m, ok := claim.(map[string]any)
		if !ok {
			continue
		}
		if m["resource"] != "accountinfos" {
			continue
		}
		hash, _ := m["identityHash"].(string)
		return hash, true
	}
	return "", false
}

func fillOrgIdpCoreIdentityHash(
	ctx context.Context,
	k8sClient ctrlruntimeclient.Client,
	path string,
	templateData map[string]any,
	log *logger.Logger,
) (unstructured.Unstructured, error) {
	apiExport := kcpapiv1alpha.APIExport{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: coreAPIExportName}, &apiExport)
	if err != nil {
		return unstructured.Unstructured{}, errors.Wrap(err, "Failed to get APIExport %s", coreAPIExportName)
	}

	hash := strings.TrimSpace(apiExport.Status.IdentityHash)
	if isPlaceholderIdentityHash(hash) {
		return unstructured.Unstructured{}, errors.New("APIExport %s IdentityHash is not set yet", coreAPIExportName)
	}

	if templateData == nil {
		templateData = map[string]any{}
	}
	templateData[apiExportCorePlatformMeshIoIdentityHashKey] = hash

	obj, err := unstructuredFromFile(path, templateData, log)
	if err != nil {
		return unstructured.Unstructured{}, err
	}

	appliedHash, found := accountInfoClaimIdentityHash(obj)
	if !found || isPlaceholderIdentityHash(appliedHash) {
		return unstructured.Unstructured{}, errors.New("refusing to apply %s with unset AccountInfo identityHash", orgIdpAPIExportName)
	}

	return obj, nil
}

func unstructuredFromFile(path string, templateData map[string]any, log *logger.Logger) (unstructured.Unstructured, error) {
	manifestBytes, err := os.ReadFile(path)
	if err != nil {
		return unstructured.Unstructured{}, errors.Wrap(err, "Failed to read file, pwd: %s", path)
	}

	res, err := ReplaceTemplate(templateData, manifestBytes)
	if err != nil {
		return unstructured.Unstructured{}, errors.Wrap(err, "Failed to replace template with path: %s", path)
	}

	var objMap map[string]any
	if err := yaml.Unmarshal(res, &objMap); err != nil {
		return unstructured.Unstructured{}, errors.Wrap(err, "Failed to unmarshal YAML from template %s. Output:\n%s", path, string(res))
	}

	obj := unstructured.Unstructured{Object: objMap}

	log.Debug().Str("file", path).Str("kind", obj.GetKind()).Str("name", obj.GetName()).Str("namespace", obj.GetNamespace()).Msg("Applying manifest")
	return obj, err
}

func GetClientAndRestConfig(kubeconfig string) (ctrlruntimeclient.Client, *rest.Config, error) {
	if kubeconfig == "" {
		config, err := rest.InClusterConfig()
		if err != nil {
			log.Error().Err(err).Msg("unable to get in-cluster deployment kubeconfig")
			return nil, nil, err
		}
		deployClient, err := ctrlruntimeclient.New(config, ctrlruntimeclient.Options{Scheme: GetClientScheme()})
		if err != nil {
			log.Error().Err(err).Msg("unable to create in-cluster deployment client")
			return nil, nil, err
		}
		return deployClient, config, nil
	}

	config, err := clientcmd.LoadFromFile(kubeconfig)
	if err != nil {
		log.Error().Err(err).Msg("unable to build Config")
		return nil, nil, err
	}
	cfgBytes, err := clientcmd.Write(*config)
	if err != nil {
		log.Error().Err(err).Msg("unable to serialize config to bytes")
		return nil, nil, err
	}
	restCfg, err := clientcmd.RESTConfigFromKubeConfig(cfgBytes)
	if err != nil {
		log.Error().Err(err).Msg("unable to build rest config from kubeconfig")
		return nil, nil, err
	}
	deployClient, err := ctrlruntimeclient.New(restCfg, ctrlruntimeclient.Options{Scheme: GetClientScheme()})
	if err != nil {
		log.Error().Err(err).Msg("unable to create client")
		return nil, nil, err
	}
	return deployClient, restCfg, nil
}

func GetClientScheme() *runtime.Scheme {
	var gvk = schema.GroupVersionKind{
		Group:   "delivery.ocm.software",
		Version: "v1alpha1",
		Kind:    "Resource",
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(pmcorev1alpha1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(appsv1.AddToScheme(scheme))
	utilruntime.Must(certmanager.AddToScheme(scheme))
	utilruntime.Must(fluxcdv1.AddToScheme(scheme))
	utilruntime.Must(fluxcdv2.AddToScheme(scheme))

	scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(gvk.GroupVersion().WithKind(gvk.Kind+"List"), &unstructured.UnstructuredList{})

	return scheme
}

func GetDeploymentTechnologyFromProfile(ctx context.Context, cl ctrlruntimeclient.Client, inst *pmcorev1alpha1.PlatformMesh) (string, error) {
	var configMapName, configMapNamespace string
	if inst.Spec.ProfileConfigMap != nil {
		configMapName = inst.Spec.ProfileConfigMap.Name
		configMapNamespace = inst.Spec.ProfileConfigMap.Namespace
		if configMapNamespace == "" {
			configMapNamespace = inst.Namespace
		}
	} else {
		configMapName = inst.Name + "-profile"
		configMapNamespace = inst.Namespace
	}

	configMap := &corev1.ConfigMap{}
	if err := cl.Get(ctx, types.NamespacedName{Name: configMapName, Namespace: configMapNamespace}, configMap); err != nil {
		return "", fmt.Errorf("failed to get profile ConfigMap %s/%s: %w", configMapNamespace, configMapName, err)
	}

	profileYAML, ok := configMap.Data[profileConfigMapKey]
	if !ok {
		return "", fmt.Errorf("profile ConfigMap %s/%s does not contain key %s", configMapNamespace, configMapName, profileConfigMapKey)
	}

	var profile map[string]any
	if err := yaml.Unmarshal([]byte(profileYAML), &profile); err != nil {
		return "", fmt.Errorf("failed to parse profile YAML from ConfigMap %s/%s: %w", configMapNamespace, configMapName, err)
	}

	if infra, ok := profile["infra"].(map[string]any); ok {
		if dt, ok := infra["deploymentTechnology"].(string); ok && dt != "" {
			return strings.ToLower(dt), nil
		}
	}

	if components, ok := profile["components"].(map[string]any); ok {
		if dt, ok := components["deploymentTechnology"].(string); ok && dt != "" {
			return strings.ToLower(dt), nil
		}
	}

	return "fluxcd", nil
}

func getExternalKcpHost(inst *pmcorev1alpha1.PlatformMesh, cfg *config.OperatorConfig) string {
	// If kcp-url is explicitly configured, use it
	if cfg.KCP.Url != "" {
		return cfg.KCP.Url
	}
	if inst.Spec.Exposure == nil {
		return fmt.Sprintf("https://%s-front-proxy.%s:%s", cfg.KCP.FrontProxyName, cfg.KCP.Namespace, cfg.KCP.FrontProxyPort)
	}
	kcpUrl := inst.Spec.Exposure.Protocol + "://" + inst.Spec.Exposure.BaseDomain + ":" + fmt.Sprintf("%d", inst.Spec.Exposure.Port)
	return kcpUrl
}
