package cmd

import (
	"flag"

	"github.com/go-logr/logr"
	openmfpconfig "github.com/openmfp/golang-commons/config"
	"github.com/openmfp/golang-commons/logger"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/openmfp/fga-operator/internal/config"
)

var (
	defaultCfg *openmfpconfig.CommonServiceConfig
	appCfg     config.Config
	v          *viper.Viper
	log        *logger.Logger
	setupLog   logr.Logger
)

var rootCmd = &cobra.Command{
	Use: "fga-operator",
}

func init() {
	rootCmd.AddCommand(initializerCmd)
	rootCmd.AddCommand(operatorCmd)
	rootCmd.AddCommand(modelGeneratorCmd)

	rootCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)

	var err error
	v, defaultCfg, err = openmfpconfig.NewDefaultConfig(rootCmd)
	if err != nil {
		panic(err)
	}

	if err := openmfpconfig.BindConfigToFlags(v, initializerCmd, &appCfg); err != nil {
		panic(err)
	}

	cobra.OnInitialize(initLog)
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
