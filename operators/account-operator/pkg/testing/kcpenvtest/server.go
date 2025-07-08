package kcpenvtest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/otiai10/copy"
	"github.com/platform-mesh/golang-commons/logger"
	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kcpapiv1alpha "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"
	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
)

const (
	kcpEnvStartTimeout        = "KCP_SERVER_START_TIMEOUT"
	kcpEnvStopTimeout         = "KCP_SERVER_STOP_TIMEOUT"
	defaultKCPServerTimeout   = 1 * time.Minute
	kcpAdminKubeconfigPath    = ".kcp/admin.kubeconfig"
	kcpRootNamespaceServerUrl = "https://localhost:6443/clusters/root"
	dirOrderPattern           = `^[0-9]*-(.*)$`
)

type Environment struct {
	kcpServer *KCPServer

	Scheme *runtime.Scheme

	ControlPlaneStartTimeout time.Duration

	ControlPlaneStopTimeout time.Duration

	Config *rest.Config

	log *logger.Logger

	RelativeSetupDirectory string

	PathToRoot             string
	RelativeAssetDirectory string

	ProviderWorkspace          string
	APIExportEndpointSliceName string

	useExistingCluster bool
}

func NewEnvironment(apiExportEndpointSliceName string, providerWorkspaceName string, pathToRoot string, relativeAssetDirectory string, relativeSetupDirectory string, useExistingCluster bool, log *logger.Logger) *Environment {
	kcpBinary := filepath.Join(relativeAssetDirectory, "kcp")
	kcpServ := NewKCPServer(pathToRoot, kcpBinary, pathToRoot, log)

	//kcpServ.Out = os.Stdout
	//kcpServ.Err = os.Stderr
	return &Environment{
		log:                        log,
		kcpServer:                  kcpServ,
		APIExportEndpointSliceName: apiExportEndpointSliceName,
		ProviderWorkspace:          providerWorkspaceName,
		RelativeSetupDirectory:     relativeSetupDirectory,
		RelativeAssetDirectory:     relativeAssetDirectory,
		PathToRoot:                 pathToRoot,
		useExistingCluster:         useExistingCluster,
	}
}

func (te *Environment) Start() (*rest.Config, string, error) {

	if !te.useExistingCluster {
		// ensure clean .kcp directory
		err := te.cleanDir()
		if err != nil {
			return nil, "", err
		}

		if err := te.defaultTimeouts(); err != nil {
			return nil, "", fmt.Errorf("failed to default controlplane timeouts: %w", err)
		}
		te.kcpServer.StartTimeout = te.ControlPlaneStartTimeout
		te.kcpServer.StopTimeout = te.ControlPlaneStopTimeout

		te.log.Info().Msg("starting control plane")
		if err := te.kcpServer.Start(); err != nil {
			return nil, "", fmt.Errorf("unable to start control plane itself: %w", err)
		}
	}

	if te.Scheme == nil {
		te.Scheme = scheme.Scheme
		utilruntime.Must(kcpapiv1alpha.AddToScheme(te.Scheme))
		utilruntime.Must(kcptenancyv1alpha.AddToScheme(te.Scheme))
	}
	//// wait for default namespace to actually be created and seen as available to the apiserver
	if err := te.waitForDefaultNamespace(); err != nil {
		return nil, "", fmt.Errorf("default namespace didn't register within deadline: %w", err)
	}

	kubectlPath := filepath.Join(te.PathToRoot, ".kcp", "admin.kubeconfig")
	var err error
	te.Config, err = clientcmd.BuildConfigFromFlags("", kubectlPath)
	if err != nil {
		return nil, "", err
	}

	if te.RelativeSetupDirectory != "" {
		// Apply all yaml files in the setup directory
		setupDirectory := filepath.Join(te.PathToRoot, te.RelativeSetupDirectory)
		kubeconfigPath := filepath.Join(te.PathToRoot, kcpAdminKubeconfigPath)
		err := te.ApplySetup(kubeconfigPath, te.Config, setupDirectory, kcpRootNamespaceServerUrl)
		if err != nil {
			return nil, "", err
		}
	}

	// Select api export
	providerServerUrl := fmt.Sprintf("%s:%s", te.Config.Host, te.ProviderWorkspace)
	te.Config.Host = providerServerUrl
	cs, err := client.New(te.Config, client.Options{})
	if err != nil {
		return nil, "", fmt.Errorf("unable to create client: %w", err)
	}

	apiExportEndpointSlice := kcpapiv1alpha.APIExportEndpointSlice{}
	err = cs.Get(context.Background(), types.NamespacedName{Name: te.APIExportEndpointSliceName}, &apiExportEndpointSlice)
	if err != nil {
		return nil, "", err
	}

	if len(apiExportEndpointSlice.Status.APIExportEndpoints) == 0 {
		return nil, "", fmt.Errorf("no virtual workspaces found")
	}

	te.Config.Host = kcpRootNamespaceServerUrl
	te.Config.QPS = 1000.0
	te.Config.Burst = 2000.0

	return te.Config, apiExportEndpointSlice.Status.APIExportEndpoints[0].URL, nil
}

func (te *Environment) Stop(useExistingCluster bool) error {
	if !useExistingCluster {
		defer te.cleanDir() //nolint:errcheck
		return te.kcpServer.Stop()
	}
	return nil
}

func (te *Environment) cleanDir() error {
	kcpPath := filepath.Join(te.PathToRoot, ".kcp")
	return os.RemoveAll(kcpPath)
}

func (te *Environment) waitForDefaultNamespace() error {
	kubectlPath := filepath.Join(te.PathToRoot, ".kcp", "admin.kubeconfig")
	config, err := clientcmd.BuildConfigFromFlags("", kubectlPath)
	if err != nil {
		return err
	}
	cs, err := client.New(config, client.Options{})
	if err != nil {
		return fmt.Errorf("unable to create client: %w", err)
	}
	// It shouldn't take longer than 5s for the default namespace to be brought up in etcd
	return wait.PollUntilContextTimeout(context.TODO(), time.Millisecond*50, time.Second*10, true, func(ctx context.Context) (bool, error) {
		te.log.Info().Msg("waiting for default namespace")
		if err = cs.Get(ctx, types.NamespacedName{Name: "default"}, &corev1.Namespace{}); err != nil {
			te.log.Info().Msg("namespace not found")
			return false, nil //nolint:nilerr
		}
		return true, nil
	})
}

func (te *Environment) waitForWorkspace(client client.Client, name string, log *logger.Logger) error {
	// It shouldn't take longer than 5s for the default namespace to be brought up in etcd
	err := wait.PollUntilContextTimeout(context.TODO(), time.Millisecond*500, time.Second*15, true, func(ctx context.Context) (bool, error) {
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

func (te *Environment) defaultTimeouts() error {
	var err error
	if te.ControlPlaneStartTimeout == 0 {
		if envVal := os.Getenv(kcpEnvStartTimeout); envVal != "" {
			te.ControlPlaneStartTimeout, err = time.ParseDuration(envVal)
			if err != nil {
				return err
			}
		} else {
			te.ControlPlaneStartTimeout = defaultKCPServerTimeout
		}
	}

	if te.ControlPlaneStopTimeout == 0 {
		if envVal := os.Getenv(kcpEnvStopTimeout); envVal != "" {
			te.ControlPlaneStopTimeout, err = time.ParseDuration(envVal)
			if err != nil {
				return err
			}
		} else {
			te.ControlPlaneStopTimeout = defaultKCPServerTimeout
		}
	}
	return nil
}

type TemplateParameters struct {
	ApiExportRootTenancyKcpIoIdentityHash  string `json:"apiExportRootTenancyKcpIoIdentityHash"`
	ApiExportRootTopologyKcpIoIdentityHash string `json:"apiExportRootTopologyKcpIoIdentityHash"`
	ApiExportRootShardsKcpIoIdentityHash   string `json:"apiExportRootShardsKcpIoIdentityHash"`
}

func (te *Environment) ApplySetup(pathToRootConfig string, config *rest.Config, setupDirectoryPath string, serverUrl string) error {

	dataFile := filepath.Join(te.PathToRoot, ".kcp/data.json")

	err := generateTemplateDataFile(config, dataFile)
	if err != nil {
		return err
	}

	// Copy setup dir
	tmpSetupDir := filepath.Join(te.PathToRoot, ".kcp/setup")
	err = os.Mkdir(tmpSetupDir, 0755)
	if err != nil {
		return err
	}
	err = copy.Copy(setupDirectoryPath, tmpSetupDir)
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpSetupDir) //nolint:errcheck

	// Apply Gomplate recursively
	err = applyTemplate(te.PathToRoot, tmpSetupDir, dataFile)
	if err != nil {
		return err
	}

	return te.ApplyYAML(pathToRootConfig, config, tmpSetupDir, serverUrl)

}

func applyTemplate(pathToRoot string, dir string, dataFile string) error {
	gomplateBinary := filepath.Join(pathToRoot, "bin", "gomplate")
	files, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, file := range files {
		if file.IsDir() {
			err := applyTemplate(pathToRoot, filepath.Join(dir, file.Name()), dataFile)
			if err != nil {
				return err
			}
		} else {
			if strings.HasSuffix(file.Name(), ".yaml") {
				filePath := filepath.Join(dir, file.Name())
				gomplateCmd := exec.Command(gomplateBinary, "-f", filePath, "-c", "data="+dataFile, "-o", filePath)
				gomplateCmd.Stdout = os.Stdout
				gomplateCmd.Stderr = os.Stderr
				if err := gomplateCmd.Run(); err != nil {
					return err
				}

			}
		}
	}
	return nil

}

func generateTemplateDataFile(config *rest.Config, dataFile string) error {
	// Collect Variables
	cs, err := client.New(config, client.Options{})
	if err != nil {
		return fmt.Errorf("unable to create client: %w", err)
	}

	parameters := TemplateParameters{}
	apiExport := kcpapiv1alpha.APIExport{}
	err = cs.Get(context.Background(), types.NamespacedName{Name: "tenancy.kcp.io"}, &apiExport)
	if err != nil {
		return err
	}
	parameters.ApiExportRootTenancyKcpIoIdentityHash = apiExport.Status.IdentityHash

	err = cs.Get(context.Background(), types.NamespacedName{Name: "shards.core.kcp.io"}, &apiExport)
	if err != nil {
		return err
	}
	parameters.ApiExportRootShardsKcpIoIdentityHash = apiExport.Status.IdentityHash

	err = cs.Get(context.Background(), types.NamespacedName{Name: "topology.kcp.io"}, &apiExport)
	if err != nil {
		return err
	}
	parameters.ApiExportRootTopologyKcpIoIdentityHash = apiExport.Status.IdentityHash

	bytes, err := json.Marshal(parameters)
	if err != nil {
		return err
	}

	err = os.WriteFile(dataFile, bytes, 0644)
	if err != nil {
		return err
	}
	return nil
}

func (te *Environment) ApplyYAML(pathToRootConfig string, config *rest.Config, pathToSetupDir string, serverUrl string) error {
	cs, err := client.New(config, client.Options{})
	if err != nil {
		return fmt.Errorf("unable to create client: %w", err)
	}

	// list directory
	hasManifestFiles, err := hasManifestFiles(pathToSetupDir)
	if err != nil {
		return err
	}
	if hasManifestFiles {
		err = te.runTemplatedKubectlCommand(pathToRootConfig, serverUrl, fmt.Sprintf("apply -f %s", pathToSetupDir), true)
		if err != nil {
			return err
		}
	}
	files, err := os.ReadDir(pathToSetupDir)
	if err != nil {
		return err
	}

	for _, file := range files {
		if file.IsDir() {
			fileName := file.Name()
			// check if pathToSetupDir starts with `[0-9]*-`
			re := regexp.MustCompile(dirOrderPattern)

			if re.Match([]byte(fileName)) {
				match := re.FindStringSubmatch(fileName)
				fileName = match[1]
			}
			err := te.waitForWorkspace(cs, fileName, te.log)
			if err != nil {
				return err
			}
			newServerUrl := fmt.Sprintf("%s:%s", serverUrl, fileName)
			wsConfig := rest.CopyConfig(config)
			wsConfig.Host = newServerUrl
			subDir := filepath.Join(pathToSetupDir, file.Name())
			err = te.ApplyYAML(pathToRootConfig, wsConfig, subDir, newServerUrl)
			if err != nil {
				return err
			}
		}
	}
	log.Info().Msg("finished applying setup")
	return nil
}

func hasManifestFiles(path string) (bool, error) {
	files, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".yaml") || strings.HasSuffix(file.Name(), ".yml") || strings.HasSuffix(file.Name(), ".json") {
			return true, nil
		}
	}
	return false, nil
}

func (te *Environment) runTemplatedKubectlCommand(kubeconfig string, server string, command string, retry bool) error {
	splitCommand := strings.Split(command, " ")
	args := []string{fmt.Sprintf("--kubeconfig=%s", kubeconfig), fmt.Sprintf("--server=%s", server)}
	args = append(args, splitCommand...)
	cmd := exec.Command("kubectl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		if retry {
			time.Sleep(5 * time.Second)
			return te.runTemplatedKubectlCommand(kubeconfig, server, command, false)
		}
		return err
	}
	return nil
}
