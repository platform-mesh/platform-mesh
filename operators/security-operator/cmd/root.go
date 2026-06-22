package cmd

import (
	"errors"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	platformeshconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/security-operator/internal/config"
	ctrl "sigs.k8s.io/controller-runtime"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	defaultCfg     *platformeshconfig.CommonServiceConfig
	initializerCfg config.Config
	terminatorCfg  config.Config
	operatorCfg    config.Config
	generatorCfg   config.Config
	systemCfg      config.Config
	log            *logger.Logger
	setupLog       logr.Logger
)

var rootCmd = &cobra.Command{
	Use: "security-operator",
}

func init() {
	rootCmd.AddCommand(initializerCmd)
	rootCmd.AddCommand(terminatorCmd)
	rootCmd.AddCommand(operatorCmd)
	rootCmd.AddCommand(modelGeneratorCmd)
	rootCmd.AddCommand(initContainerCmd)
	rootCmd.AddCommand(systemCmd)

	defaultCfg = platformeshconfig.NewDefaultConfig()
	operatorCfg = config.NewConfig()
	generatorCfg = config.NewConfig()
	initializerCfg = config.NewConfig()
	terminatorCfg = config.NewConfig()
	systemCfg = config.NewConfig()
	initContainerCfg = config.NewInitContainerConfig()

	defaultCfg.AddFlags(rootCmd.PersistentFlags())
	operatorCfg.AddFlags(operatorCmd.Flags())
	generatorCfg.AddFlags(modelGeneratorCmd.Flags())
	initializerCfg.AddFlags(initializerCmd.Flags())
	terminatorCfg.AddFlags(terminatorCmd.Flags())
	systemCfg.AddFlags(systemCmd.Flags())
	initContainerCfg.AddFlags(initContainerCmd.Flags())

	cobra.OnInitialize(initLog)
}

func getKubeconfigFromPath(kubeconfigPath string) (*rest.Config, error) {
	if kubeconfigPath == "" {
		return nil, errors.New("missing value for required flag --kcp-kubeconfig")
	}
	cfg, err := clientcmd.LoadFromFile(kubeconfigPath)
	if err != nil {
		return nil, err
	}
	restCfg, err := clientcmd.NewDefaultClientConfig(*cfg, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return restCfg, err
	}
	return restCfg, nil
}

func initLog() { // coverage-ignore
	logcfg := logger.DefaultConfig()
	logcfg.Level = defaultCfg.Log.Level
	logcfg.NoJSON = defaultCfg.Log.NoJson

	var err error
	log, err = logger.New(logcfg)
	if err != nil {
		panic(err)
	}

	ctrl.SetLogger(log.Logr())
	setupLog = ctrl.Log.WithName("setup") // coverage-ignore
}

func Execute() {
	cobra.CheckErr(rootCmd.Execute())
}
