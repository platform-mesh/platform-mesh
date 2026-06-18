package config

import (
	"github.com/spf13/pflag"

	corev1 "k8s.io/api/core/v1"
)

type InitContainerClientConfig struct {
	Name      string                 `mapstructure:"name" yaml:"name"`
	SecretRef corev1.SecretReference `mapstructure:"secretRef" yaml:"secretRef"`
}

type InitContainerConfiguration struct {
	KeycloakBaseURL  string                      `mapstructure:"keycloakBaseURL"`
	KeycloakClientID string                      `mapstructure:"keycloakClientID" default:"admin-cli"`
	KeycloakUser     string                      `mapstructure:"keycloakUser" default:"admin"`
	PasswordFile     string                      `mapstructure:"passwordFile" default:"/secrets/keycloak-password"`
	Clients          []InitContainerClientConfig `mapstructure:"clients"`
}

type InitContainerConfig struct {
	ConfigFile string `mapstructure:"config-file" default:"/config/config.yaml"`
}

func NewInitContainerConfig() InitContainerConfig {
	return InitContainerConfig{
		ConfigFile: "/config/config.yaml",
	}
}

func (c *InitContainerConfig) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&c.ConfigFile, "config-file", c.ConfigFile, "Path to init-container YAML configuration")
}
