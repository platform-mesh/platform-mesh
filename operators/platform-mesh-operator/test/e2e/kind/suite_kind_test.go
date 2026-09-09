//go:build e2e

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

package e2e

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	goruntime "runtime"
	"strings"
	"time"

	certmanager "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/creasty/defaults"
	fluxcdv2 "github.com/fluxcd/helm-controller/api/v2"
	fluxcdv1 "github.com/fluxcd/source-controller/api/v1beta2"
	"github.com/stretchr/testify/suite"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmprovidersv1alpha1 "go.platform-mesh.io/apis/providers/v1alpha1"
	pmconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/context/keys"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/platform-mesh-operator/internal/config"
	"go.platform-mesh.io/platform-mesh-operator/internal/controller"
	providerscontroller "go.platform-mesh.io/platform-mesh-operator/internal/controller/providers"
	"go.platform-mesh.io/platform-mesh-operator/pkg/kapply"
	"go.platform-mesh.io/platform-mesh-operator/pkg/subroutines"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcmultiprovider "sigs.k8s.io/multicluster-runtime/providers/multi"

	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
)

type KindTestSuite struct {
	suite.Suite
	client ctrlruntimeclient.Client
	config *rest.Config
	scheme *runtime.Scheme
	logger *logger.Logger
	mgr    mcmanager.Manager

	containerRuntime string

	cancel context.CancelFunc
}

var clusterName = "platform-mesh"

var defaultKcpOperatorConfig = config.KCPConfig{
	Url:                    "https://root.kcp.localhost:8443",
	RootShardName:          "root",
	Namespace:              "platform-mesh-system",
	FrontProxyName:         "frontproxy",
	FrontProxyPort:         "8443",
	ClusterAdminSecretName: "kcp-cluster-admin-client-cert",
}

func kindBinary() string {
	if bin := os.Getenv("KIND_BINARY"); bin != "" {
		return bin
	}

	return "kind"
}

func mkcertBinary() string {
	if bin := os.Getenv("MKCERT_BINARY"); bin != "" {
		return bin
	}

	return "mkcert"
}

// runCommand executes a shell command and returns its output.
func runCommand(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Stderr = os.Stderr

	// Get the current environment
	env := os.Environ()
	// Add or override specific variables
	goos := goruntime.GOOS
	if goos == "darwin" {
		env = append(env, "DOCKER_HOST=unix:///var/run/docker.sock")
	} else {
		env = append(env, "DOCKER_HOST=unix:///run/docker.sock")
	}
	cmd.Env = env

	return cmd.Output()
}

// checkClusterExists checks if a Kind cluster with the given name exists.
func checkClusterExists(clusterName string) (bool, error) {
	output, err := runCommand(kindBinary(), "get", "clusters")
	if err != nil {
		return false, fmt.Errorf("failed to get Kind clusters: %w", err)
	}

	if strings.Contains(string(output), clusterName) {
		return true, nil
	}
	return false, nil
}

// createKubernetesClient creates a Kubernetes client from the given kubeconfig.
func createKubernetesClient(kubeconfig []byte, s *runtime.Scheme) (ctrlruntimeclient.Client, *rest.Config, error) {
	config, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create REST config from kubeconfig: %w", err)
	}

	k8sClient, err := ctrlruntimeclient.New(config, ctrlruntimeclient.Options{
		Scheme: s,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	return k8sClient, config, nil
}

func (s *KindTestSuite) createLogger() error {
	logConfig := logger.DefaultConfig()
	logConfig.NoJSON = true
	logConfig.Level = "debug"
	logConfig.Name = "KindTestSuite"

	log, err := logger.New(logConfig)
	if err != nil {
		return err
	}

	s.logger = log
	ctrl.SetLogger(s.logger.Logr())

	return nil
}

func (s *KindTestSuite) detectContainerRuntime() error {
	s.logger.Info().Msg("Checking whether Docker is available...")
	_, err := runCommand("docker", "--version")
	if err == nil {
		s.logger.Info().Msg("Docker found")
		s.containerRuntime = "docker"
		return nil
	}

	s.logger.Info().Msg("Checking whether Podman is available...")
	_, err = runCommand("podman", "--version")
	if err == nil {
		s.logger.Info().Msg("Podman found")
		s.containerRuntime = "podman"
		return nil
	}

	return fmt.Errorf("no container runtime found in PATH: install Docker or Podman")
}

func (s *KindTestSuite) createKindCluster() error {
	// Check if Kind cluster already exists if not create it
	s.logger.Info().Msg("Checking if Kind cluster exists...")

	clusterExists, err := checkClusterExists(clusterName)
	if err != nil {
		return err
	}

	if clusterExists {
		s.logger.Info().Msg("Kind cluster already exists, skipping creation")
	} else {
		s.logger.Info().Msg("Creating Kind cluster...")
		if out, err := runCommand(s.containerRuntime, "system", "prune", "-f"); err != nil {
			return errors.Join(err, errors.New(string(out)))
		}

		if out, err := runCommand(s.containerRuntime, "ps", "-a"); err != nil {
			return errors.Join(err, errors.New(string(out)))
		}

		if out, err := runCommand(kindBinary(), "--version"); err != nil {
			return errors.Join(err, errors.New(string(out)))
		}

		s.logger.Info().Msg("Creating Kind cluster...")
		if _, err = runCommand(kindBinary(), "create", "cluster", "--config", "../../../kind-config.yaml", "--name", clusterName, "--image=kindest/node:v1.30.2"); err != nil {
			return err
		}
	}

	// Get kubeconfig for the Kind cluster
	s.logger.Info().Msg("Retrieving kubeconfig for Kind cluster...")
	var kubeconfig []byte
	if kubeconfig, err = runCommand(kindBinary(), "get", "kubeconfig", "--name", clusterName); err != nil {
		return err
	}

	// register scheme
	s.scheme = runtime.NewScheme()
	utilruntime.Must(pmcorev1alpha1.AddToScheme(s.scheme))
	utilruntime.Must(fluxcdv2.AddToScheme(s.scheme))
	utilruntime.Must(corev1.AddToScheme(s.scheme))
	utilruntime.Must(rbacv1.AddToScheme(s.scheme))
	utilruntime.Must(appsv1.AddToScheme(s.scheme))
	utilruntime.Must(certmanager.AddToScheme(s.scheme))
	utilruntime.Must(fluxcdv1.AddToScheme(s.scheme))
	utilruntime.Must(fluxcdv2.AddToScheme(s.scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(s.scheme))
	utilruntime.Must(pmprovidersv1alpha1.AddToScheme(s.scheme))
	utilruntime.Must(kcpapisv1alpha1.AddToScheme(s.scheme))
	utilruntime.Must(kcpapisv1alpha2.AddToScheme(s.scheme))
	utilruntime.Must(kcptenancyv1alpha.AddToScheme(s.scheme))

	gvk := fluxcdv2.GroupVersion.WithKind("HelmRelease")
	s.logger.Info().Msgf("Registering GVK: %s", gvk.String())

	if _, err = s.scheme.New(gvk); err != nil {
		return err
	}

	// Pass kubeconfig directly to the Kubernetes client
	s.logger.Info().Msg("Creating Kubernetes client using kubeconfig...")
	cl, configClient, err := createKubernetesClient(kubeconfig, s.scheme)
	if err != nil {
		return err
	}

	s.client = cl
	s.config = configClient

	pods := &corev1.PodList{}
	err = s.client.List(s.T().Context(), pods, &ctrlruntimeclient.ListOptions{
		Namespace: "kube-system",
	})
	if err != nil {
		return err
	}
	if len(pods.Items) == 0 {
		return errors.New("No pods found in kube-system namespace, this might be an issue")
	}
	return nil
}

func (s *KindTestSuite) createCerts() ([]byte, error) {
	// mkcert
	_, err := runCommand("mkdir", "-p", "certs")
	s.Require().NoError(err, "Error creating certs directory")
	if _, err = runCommand(mkcertBinary(), "-cert-file=certs/cert.crt", "-key-file=certs/cert.key", "portal.localhost", "*.portal.localhost", "localhost", "kcp.localhost", "*.kcp.localhost"); err != nil {
		return nil, err
	}
	dirRootPath, err := runCommand(mkcertBinary(), "-CAROOT")
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to get mkcert CAROOT")
		return nil, err
	}

	// generate webhook certificates
	if _, err := runCommand("scripts/gen-certs.sh"); err != nil {
		return nil, err
	}

	return dirRootPath, nil
}

func (s *KindTestSuite) createSecrets(ctx context.Context, dirRootPath []byte) error {
	carootPath := fmt.Sprintf("%s/rootCA.pem", strings.TrimSuffix(string(dirRootPath), "\n"))
	var caRootBytes []byte
	var err error
	if caRootBytes, err = os.ReadFile(carootPath); err != nil {
		return err
	}
	certBytes, err := os.ReadFile("certs/cert.crt")
	if err != nil {
		return err
	}
	keyBytes, err := os.ReadFile("certs/cert.key")
	if err != nil {
		return err
	}
	caIamRootCABytes, err := os.ReadFile("webhook-config/ca.crt")
	if err != nil {
		return err
	}
	iamCertBytes, err := os.ReadFile("webhook-config/tls.crt")
	if err != nil {
		return err
	}
	iamKeyBytes, err := os.ReadFile("webhook-config/tls.key")
	if err != nil {
		return err
	}

	keycloak_admin := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "keycloak-admin",
			Namespace: "platform-mesh-system",
		},
		Data: map[string][]byte{
			"secret": []byte("admin"),
		},
		Type: corev1.SecretTypeOpaque,
	}
	domain_certificate := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "domain-certificate",
			Namespace: "istio-system",
		},
		Data: map[string][]byte{
			"ca.crt":  caRootBytes,
			"tls.crt": certBytes,
			"tls.key": keyBytes,
		},
		Type: corev1.SecretTypeTLS,
	}
	pms_domain_certificate := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "domain-certificate",
			Namespace: "platform-mesh-system",
		},
		Data: map[string][]byte{
			"ca.crt":  caRootBytes,
			"tls.crt": certBytes,
			"tls.key": keyBytes,
		},
		Type: corev1.SecretTypeTLS,
	}
	rbac_webhook_ca := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rebac-authz-webhook-ca",
			Namespace: "platform-mesh-system",
		},
		Data: map[string][]byte{
			"ca.crt":  caIamRootCABytes,
			"tls.crt": iamCertBytes,
			"tls.key": iamKeyBytes,
		},
		Type: corev1.SecretTypeTLS,
	}
	securityOperatorCa := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "security-operator-ca-secret",
			Namespace: "platform-mesh-system",
		},
		Data: map[string][]byte{
			"ca.crt": caRootBytes,
		},
		Type: corev1.SecretTypeOpaque,
	}
	domain_certificate_ca := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "domain-certificate-ca",
			Namespace: "platform-mesh-system",
		},
		Data: map[string][]byte{
			"tls.crt": caRootBytes,
		},
		Type: corev1.SecretTypeOpaque,
	}
	createIfNotExists := func(obj ctrlruntimeclient.Object) error {
		if err := s.client.Create(ctx, obj); err != nil {
			if apierrors.IsAlreadyExists(err) {
				return nil
			}
			return err
		}
		return nil
	}

	secrets := []ctrlruntimeclient.Object{
		keycloak_admin,
		domain_certificate,
		rbac_webhook_ca,
		securityOperatorCa,
		domain_certificate_ca,
		pms_domain_certificate,
	}

	for _, sec := range secrets {
		if err := createIfNotExists(sec); err != nil {
			return err
		}
	}
	return nil
}

func (s *KindTestSuite) createReleases(ctx context.Context) error {
	if err := ApplyManifestFromFile(ctx, "../../../test/e2e/kind/yaml/flux2-v2.6.4/flux2-install.yaml", s.client, make(map[string]string)); err != nil {
		return err
	}
	avail := s.Eventually(func() bool {
		deployment := &appsv1.Deployment{}

		err := s.client.Get(ctx, ctrlruntimeclient.ObjectKey{
			Name:      "helm-controller",
			Namespace: "flux-system",
		}, deployment)
		if err != nil {
			s.logger.Warn().Msg("Not getting helm-controller deployment")
			return false
		}
		helmControllerReady := (deployment.Status.ReadyReplicas > 0)

		err = s.client.Get(ctx, ctrlruntimeclient.ObjectKey{
			Name:      "source-controller",
			Namespace: "flux-system",
		}, deployment)
		if err != nil {
			s.logger.Warn().Msg("Not getting source-controller deployment")
			return false
		}
		sourceControllerReady := (deployment.Status.ReadyReplicas > 0)

		return helmControllerReady && sourceControllerReady
	}, 240*time.Second, 5*time.Second, "helm resources did not become ready")

	if !avail {
		return errors.New("helm resources are not available")
	}

	s.logger.Info().Msg("helm resources ready")

	time.Sleep(25 * time.Second)
	return nil
}

// SetupSuite sets up the Kind cluster and deploys the operator for testing.
func (s *KindTestSuite) SetupSuite() {
	ctx := context.Background()
	var err error

	if err = s.createLogger(); err != nil {
		s.logger.Error().Err(err).Msg("Failed to create logger")
		s.T().FailNow()
	}
	if err = s.detectContainerRuntime(); err != nil {
		s.logger.Error().Err(err).Msg("Failed to detect container runtime")
		s.T().FailNow()
	}
	if err = s.createKindCluster(); err != nil {
		s.logger.Error().Err(err).Msg("Failed to create Kind cluster")
		s.T().FailNow()
	}
	if err = s.InstallCRDs(ctx); err != nil {
		s.logger.Error().Err(err).Msg("Failed to install CRDs")
		s.T().FailNow()
	}
	var dirRootPath []byte
	if dirRootPath, err = s.createCerts(); err != nil {
		s.logger.Error().Err(err).Msg("Failed to create certificates")
		s.T().FailNow()
	}
	if err = ApplyManifestFromFile(ctx, "../../../test/e2e/kind/yaml/namespaces.yaml", s.client, make(map[string]string)); err != nil {
		s.logger.Error().Err(err).Msg("Failed to apply namespaces.yaml manifest")
		s.T().FailNow()
	}
	if err = s.createSecrets(ctx, dirRootPath); err != nil {
		s.logger.Error().Err(err).Msg("Failed to create secrets")
		s.T().FailNow()
	}
	if err = s.createReleases(ctx); err != nil {
		s.logger.Error().Err(err).Msg("Failed to create releases")
		s.T().FailNow()
	}
	if err = s.applyKustomize(ctx); err != nil {
		s.FailNow("Failed to apply kustomize manifests")
	}
	if err = s.waitForCRDEstablished(ctx, "repositories.delivery.ocm.software", 2*time.Minute); err != nil {
		s.FailNow("OCM Repository CRD not established in time")
	}
	s.logger.Info().Msg("repositories.delivery.ocm.software CRD established")
	if err = s.waitForCRDEstablished(ctx, "components.delivery.ocm.software", 2*time.Minute); err != nil {
		s.FailNow("OCM Component CRD not established in time")
	}
	s.logger.Info().Msg("components.delivery.ocm.software CRD established")
	if err = s.waitForCRDEstablished(ctx, "resources.delivery.ocm.software", 5*time.Minute); err != nil {
		s.FailNow("OCM Resource CRD not established in time")
	}
	s.logger.Info().Msg("resources.delivery.ocm.software CRD established")

	if err = s.applyOCM(ctx); err != nil {
		s.FailNow("Failed to apply OCM manifests", err)
	}

	// add Platform Mesh profile ConfigMap
	if err = ApplyManifestFromFile(ctx, "../../../test/e2e/kind/yaml/platform-mesh-resource/default-profile.yaml", s.client, make(map[string]string)); err != nil {
		s.FailNow("Failed to apply PlatformMesh profile ConfigMap manifest", err)
	}

	// add Platform Mesh resource
	if err = ApplyManifestFromFile(ctx, "../../../test/e2e/kind/yaml/platform-mesh-resource/platform-mesh.yaml", s.client, make(map[string]string)); err != nil {
		s.FailNow("Failed to apply PlatformMesh resource manifest", err)
	}

	avail := s.Eventually(func() bool {
		pm := pmcorev1alpha1.PlatformMesh{}
		err := s.client.Get(ctx, ctrlruntimeclient.ObjectKey{
			Name:      "platform-mesh",
			Namespace: "platform-mesh-system",
		}, &pm)
		if err != nil {
			s.logger.Warn().Err(err).Msg("Failed to get PlatformMesh resource")
			return false
		}
		return true
	}, 15*time.Second, 2*time.Second, "PlatformMesh resource did not become available")

	if !avail {
		s.logger.Error().Msg("PlatformMesh resource is not available")
		s.T().FailNow()
	}

	// Run the PlatformMesh operator
	s.logger.Info().Msg("starting PlatformMesh operator...")
	s.runPlatformMeshOperator(ctx)

	if err = s.addFrontProxyCertificateSAN(ctx, "root.kcp.localhost"); err != nil {
		s.logger.Error().Err(err).Msg("Failed to add front-proxy certificate SAN")
		s.T().FailNow()
	}
}

// addFrontProxyCertificateSAN adds an extra DNS name to the FrontProxy's
// server certificate. The "infra" chart's kcp.certificateTemplates value
// doesn't actually reach FrontProxy.spec, so this patches it directly.
func (s *KindTestSuite) addFrontProxyCertificateSAN(ctx context.Context, dnsName string) error {
	fpGVK := "operator.kcp.io/v1alpha1"
	frontProxy := &unstructured.Unstructured{}
	frontProxy.SetAPIVersion(fpGVK)
	frontProxy.SetKind("FrontProxy")

	err := wait.PollUntilContextTimeout(ctx, 5*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		getErr := s.client.Get(ctx, ctrlruntimeclient.ObjectKey{Name: "frontproxy", Namespace: "platform-mesh-system"}, frontProxy)
		return getErr == nil, nil
	})
	if err != nil {
		return fmt.Errorf("waiting for FrontProxy resource: %w", err)
	}

	patch := ctrlruntimeclient.RawPatch(types.MergePatchType, []byte(fmt.Sprintf(
		`{"spec":{"certificateTemplates":{"server":{"spec":{"dnsNames":[%q]}}}}}`, dnsName)))
	if err := s.client.Patch(ctx, frontProxy, patch); err != nil {
		return fmt.Errorf("patching FrontProxy certificateTemplates: %w", err)
	}

	cert := &certmanager.Certificate{}
	err = wait.PollUntilContextTimeout(ctx, 5*time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		if getErr := s.client.Get(ctx, ctrlruntimeclient.ObjectKey{Name: "root-frontproxy-server", Namespace: "platform-mesh-system"}, cert); getErr != nil {
			return false, nil
		}
		for _, name := range cert.Spec.DNSNames {
			if name == dnsName {
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("waiting for front-proxy certificate to pick up %s: %w", dnsName, err)
	}

	s.logger.Info().Str("dnsName", dnsName).Msg("front-proxy certificate now covers extra hostname")
	return nil
}

func (s *KindTestSuite) waitForCRDEstablished(ctx context.Context, crdName string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		crd := &apiextensionsv1.CustomResourceDefinition{}
		err := s.client.Get(ctx, ctrlruntimeclient.ObjectKey{Name: crdName}, crd)
		if err != nil {
			return false, nil //nolint:nilerr
		}

		for _, condition := range crd.Status.Conditions {
			if condition.Type == apiextensionsv1.Established {
				if condition.Status == apiextensionsv1.ConditionTrue {
					s.logger.Info().Msgf("CRD %s is established", crdName)
					return true, nil
				}
			}
		}
		s.logger.Debug().Msgf("CRD %s not established yet", crdName)
		return false, nil
	})
}

// applyOCM applies the OCM component and repository to the cluster.
func (s *KindTestSuite) applyOCM(ctx context.Context) error {
	clients, err := kapply.NewClients(s.config)
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to create kapply clients")
		return err
	}

	return kapply.ApplyDir(ctx, "../../../test/e2e/kind/kustomize/components/ocm", clients)
}

func (s *KindTestSuite) applyKustomize(ctx context.Context) error {
	clients, err := kapply.NewClients(s.config)
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to create kapply clients")
		return err
	}

	err = kapply.ApplyDir(ctx, "../../../test/e2e/kind/kustomize/base", clients)
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to apply base kustomize manifests")
		return err
	}

	// err = kapply.ApplyDir(ctx, "../../../test/e2e/kind/kustomize/base/kro", clients)
	// if err != nil {
	// 	s.logger.Error().Err(err).Msg("Failed to apply kro kustomize manifests")
	// 	return err
	// }

	s.logger.Info().Msg("kapply finished successfully")
	return nil
}

func (s *KindTestSuite) TearDownSuite() {
}

func (s *KindTestSuite) InstallCRDs(ctx context.Context) error {
	err := ApplyManifestFromFile(ctx, "../../../config/crd/core.platform-mesh.io_platformmeshes.yaml", s.client, make(map[string]string))
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to apply PlatformMesh CRD manifest")
		return err
	}

	if err := ApplyManifestFromFile(ctx, "../../../config/crd/providers.platform-mesh.io_managedproviders.yaml", s.client, make(map[string]string)); err != nil {
		s.logger.Error().Err(err).Msg("Failed to apply ManagedProvider CRD manifest")
		return err
	}

	return nil
}

func (s *KindTestSuite) runPlatformMeshOperator(ctx context.Context) {
	appConfig := config.NewOperatorConfig()

	err := defaults.Set(&appConfig)
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to set default PlatformMesh operator config")
		return
	}

	appConfig.Subroutines.Deployment.Enabled = true
	appConfig.Subroutines.Deployment.EnableIstio = false
	appConfig.Subroutines.KcpSetup.Enabled = true
	appConfig.Subroutines.ProviderSecret.Enabled = true
	appConfig.Subroutines.FeatureToggles.Enabled = true
	appConfig.Subroutines.ManagedProvider.WaitPlatformMesh.Enabled = true
	appConfig.Subroutines.ManagedProvider.ProviderResource.Enabled = true
	appConfig.Subroutines.ManagedProvider.WaitProvider.Enabled = true
	appConfig.Subroutines.ManagedProvider.KubeconfigCopy.Enabled = true
	appConfig.Subroutines.ManagedProvider.Deploy.Enabled = true
	appConfig.Subroutines.Provider.Workspace.Enabled = true
	appConfig.Subroutines.Provider.Kubeconfig.Enabled = true
	appConfig.WorkspaceDir = "../../../"
	appConfig.KCP = defaultKcpOperatorConfig

	commonConfig := &pmconfig.CommonServiceConfig{}
	commonConfig.IsLocal = true

	ctx = context.WithValue(ctx, keys.ConfigCtxKey, appConfig)

	options := ctrl.Options{
		Scheme:      s.scheme,
		BaseContext: func() context.Context { return ctx },
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	}
	mgr, err := mcmanager.New(s.config, mcmultiprovider.New(mcmultiprovider.Options{}), options)
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to create manager")
		return
	}

	imageVersionStore := subroutines.NewImageVersionStore()
	pmReconciler, err := controller.NewPlatformMeshReconciler(mgr, &appConfig, commonConfig, "../../../", mgr.GetLocalManager().GetClient(), imageVersionStore)
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to create PlatformMesh reconciler")
		return
	}
	if err := pmReconciler.SetupWithManager(mgr, commonConfig); err != nil {
		s.logger.Error().Err(err).Msg("Failed to setup PlatformMesh reconciler with manager")
		return
	}

	resourceReconciler, err := controller.NewResourceReconciler(mgr, &appConfig, mgr.GetLocalManager().GetClient(), imageVersionStore)
	if err != nil {
		s.logger.Error().Err(err).Msg("unable to create Resource reconciler")
		return
	}
	if err := resourceReconciler.SetupWithManager(mgr, commonConfig); err != nil {
		s.logger.Error().Err(err).Msg("unable to create resource controller")
		return
	}

	managedProviderReconciler, err := providerscontroller.NewManagedProviderReconciler(mgr, &appConfig, commonConfig)
	if err != nil {
		s.logger.Error().Err(err).Msg("unable to create ManagedProvider reconciler")
		return
	}
	if err := managedProviderReconciler.SetupWithManager(mgr, commonConfig); err != nil {
		s.logger.Error().Err(err).Msg("unable to setup ManagedProvider controller")
		return
	}

	go func() {
		var controllerContext context.Context
		controllerContext, s.cancel = context.WithCancel(context.Background())
		err := mgr.Start(controllerContext)
		s.Nil(err)
	}()
	s.logger.Info().Msg("PlatformMesh operator started")

	s.mgr = mgr
}
