package cmd

import (
	"errors"
	"flag"
	"strings"

	"github.com/go-logr/logr"
	platformeshconfig "github.com/platform-mesh/golang-commons/config"
	"github.com/platform-mesh/golang-commons/logger"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/platform-mesh/security-operator/internal/config"
)

var (
	defaultCfg     *platformeshconfig.CommonServiceConfig
	initializerCfg config.Config
	operatorCfg    config.Config
	generatorCfg   config.Config
	log            *logger.Logger
	setupLog       logr.Logger
)

var rootCmd = &cobra.Command{
	Use: "security-operator",
}

func init() {
	rootCmd.AddCommand(initializerCmd)
	rootCmd.AddCommand(operatorCmd)
	rootCmd.AddCommand(modelGeneratorCmd)

	rootCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)

	var err error
	_, defaultCfg, err = platformeshconfig.NewDefaultConfig(rootCmd)
	if err != nil {
		panic(err)
	}

	operatorV := newViper()
	if err := platformeshconfig.BindConfigToFlags(operatorV, operatorCmd, &operatorCfg); err != nil {
		panic(err)
	}
	generatorV := newViper()
	if err := platformeshconfig.BindConfigToFlags(generatorV, modelGeneratorCmd, &generatorCfg); err != nil {
		panic(err)
	}
	initializerV := newViper()
	if err := platformeshconfig.BindConfigToFlags(initializerV, initializerCmd, &initializerCfg); err != nil {
		panic(err)
	}

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

func newViper() *viper.Viper {
	v := viper.NewWithOptions(
		viper.EnvKeyReplacer(strings.NewReplacer("-", "_")),
	)

	v.AutomaticEnv()
	return v
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
