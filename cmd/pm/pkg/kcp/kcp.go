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

// Package kcp runs a kcp shard with embedded etcd inside the process, the
// way "kcp start" does.
package kcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	cliflag "k8s.io/component-base/cli/flag"
	logsapiv1 "k8s.io/component-base/logs/api/v1"
	kubeoptions "k8s.io/kubernetes/pkg/kubeapiserver/options"
	"sigs.k8s.io/yaml"

	"github.com/kcp-dev/embeddedetcd"
	kcpoptions "github.com/kcp-dev/kcp/cmd/kcp/options"
	kcpfeatures "github.com/kcp-dev/kcp/pkg/features"
	"github.com/kcp-dev/kcp/pkg/server"
	"github.com/kcp-dev/sdk/apis/core"
	kcpclientset "github.com/kcp-dev/sdk/client/clientset/versioned/cluster"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// baseContext is the admin.kubeconfig context whose server has no
// /clusters/<path> suffix.
const baseContext = "base"

// Server is an in-process kcp.
type Server struct {
	// Options are kcp's own start options; AddFlags exposes them.
	Options *kcpoptions.Options
	rootDir string
}

// New returns a kcp that keeps its state (etcd data, certificates,
// admin.kubeconfig) in rootDir.
func New(rootDir string) *Server {
	o := kcpoptions.NewOptions(rootDir)
	o.Server.GenericControlPlane.Logs.Verbosity = logsapiv1.VerbosityLevel(1)
	return &Server{Options: o, rootDir: rootDir}
}

// AddFlags adds every "kcp start" flag to fss.
func (s *Server) AddFlags(fss *cliflag.NamedFlagSets) {
	s.Options.AddFlags(fss)
}

// AdminKubeconfig is the path of the kubeconfig kcp writes for its admin.
func (s *Server) AdminKubeconfig() string {
	return s.Options.Server.AdminAuthentication.KubeConfigPath
}

// HasOIDC reports whether kcp was pointed at an OIDC issuer with flags.
func (s *Server) HasOIDC() bool {
	o := s.Options.Server.GenericControlPlane.Authentication.OIDC
	return o != nil && o.IssuerURL != ""
}

// TrustOIDC makes kcp accept the ID tokens issuer signs for clientID: the
// email claim is the user name, the groups claim the groups. It must be
// called before Run.
func (s *Server) TrustOIDC(issuer, clientID, caFile string) {
	auth := s.Options.Server.GenericControlPlane.Authentication
	if auth.OIDC == nil {
		auth.OIDC = &kubeoptions.OIDCAuthenticationOptions{}
	}
	auth.OIDC.IssuerURL = issuer
	auth.OIDC.ClientID = clientID
	auth.OIDC.CAFile = caFile
	auth.OIDC.UsernameClaim = "email"
	auth.OIDC.GroupsClaim = "groups"
	auth.OIDC.FlagsSet = true
}

// ProxyPaths makes kcp forward the request paths that are the keys of
// backends to the URLs that are their values, the way a front-proxy does
// for virtual workspaces that are not kcp's own. The caller's credentials
// are forwarded as they are. It must be called before Run.
func (s *Server) ProxyPaths(backends map[string]string) error {
	type mapping struct {
		Path    string `json:"path"`
		Backend string `json:"backend"`
	}
	var mappings []mapping
	for path, backend := range backends {
		mappings = append(mappings, mapping{Path: path, Backend: backend})
	}
	sort.Slice(mappings, func(i, j int) bool { return mappings[i].Path < mappings[j].Path })
	raw, err := yaml.Marshal(mappings)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.rootDir, 0o700); err != nil {
		return err
	}
	file := filepath.Join(s.rootDir, "proxy-mappings.yaml")
	if err := os.WriteFile(file, raw, 0o600); err != nil {
		return err
	}
	s.Options.Server.Extra.AdditionalMappingsFile = file
	return nil
}

// AuthorizeWith makes kcp ask the SubjectAccessReview webhook at url about
// the requests its own authorizers do not allow. caFile is the PEM CA of
// the webhook's serving certificate. It must be called before Run.
func (s *Server) AuthorizeWith(url, caFile string) error {
	ca, err := os.ReadFile(caFile)
	if err != nil {
		return err
	}
	kubeconfig := clientcmdapi.NewConfig()
	kubeconfig.Clusters["webhook"] = &clientcmdapi.Cluster{Server: url, CertificateAuthorityData: ca}
	kubeconfig.Contexts["webhook"] = &clientcmdapi.Context{Cluster: "webhook"}
	kubeconfig.CurrentContext = "webhook"
	if err := os.MkdirAll(s.rootDir, 0o700); err != nil {
		return err
	}
	file := filepath.Join(s.rootDir, "authz-webhook.kubeconfig")
	if err := clientcmd.WriteToFile(*kubeconfig, file); err != nil {
		return err
	}
	webhook := s.Options.Server.Authorization.Webhook
	webhook.WebhookConfigFile = file
	webhook.WebhookVersion = "v1"
	return nil
}

// Run starts embedded etcd and the kcp server and blocks until ctx is done
// or the server fails. It applies the process-wide logging configuration, so
// it must be called once.
func (s *Server) Run(ctx context.Context) error {
	if err := logsapiv1.ValidateAndApply(s.Options.Server.GenericControlPlane.Logs, kcpfeatures.DefaultFeatureGate); err != nil {
		return err
	}
	rest.SetDefaultWarningHandler(rest.NoWarnings{})

	// kcp names itself by the address of the default route, which is gone
	// when the laptop changes networks: workspaces, kubeconfigs and the
	// URLs of the virtual workspaces would point nowhere. Everything that
	// talks to this kcp is on this machine.
	if extra := &s.Options.Server.Extra; extra.ShardBaseURL == "" && extra.ShardExternalURL == "" {
		local := fmt.Sprintf("https://localhost:%d", s.Options.Server.GenericControlPlane.SecureServing.BindPort)
		extra.ShardBaseURL, extra.ShardExternalURL = local, local
	}

	completed, err := s.Options.Complete(ctx)
	if err != nil {
		return err
	}
	if errs := completed.Validate(); len(errs) > 0 {
		return utilerrors.NewAggregate(errs)
	}

	serverConfig, err := server.NewConfig(ctx, completed.Server)
	if err != nil {
		return err
	}
	completedConfig, err := serverConfig.Complete()
	if err != nil {
		return err
	}

	// etcd outlives the apiserver so that shutdown does not block on it.
	etcdCtx, etcdCancel := context.WithCancel(context.Background())
	defer etcdCancel()
	if completedConfig.EmbeddedEtcd.Config != nil {
		if err := embeddedetcd.NewServer(completedConfig.EmbeddedEtcd).Run(etcdCtx); err != nil {
			return err
		}
	}

	srv, err := server.NewServer(completedConfig)
	if err != nil {
		return err
	}
	return srv.Run(ctx)
}

// WaitReady blocks until kcp serves the root workspace and returns an admin
// rest config whose host is the bare server URL (no /clusters/<path>).
func (s *Server) WaitReady(ctx context.Context, timeout time.Duration) (*rest.Config, error) {
	var cfg *rest.Config
	var lastErr error
	err := wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, timeout, true, func(ctx context.Context) (bool, error) {
		cfg, lastErr = s.adminConfig()
		if lastErr != nil {
			return false, nil
		}
		client, err := kcpclientset.NewForConfig(cfg)
		if err != nil {
			lastErr = err
			return false, nil
		}
		// The root workspace's types are the last thing kcp bootstraps.
		if _, err := client.Cluster(core.RootCluster.Path()).TenancyV1alpha1().WorkspaceTypes().Get(ctx, "universal", metav1.GetOptions{}); err != nil {
			lastErr = err
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return nil, fmt.Errorf("kcp did not become ready: %w (last error: %v)", err, lastErr)
	}
	return cfg, nil
}

func (s *Server) adminConfig() (*rest.Config, error) {
	path := s.AdminKubeconfig()
	if !filepath.IsAbs(path) {
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(wd, path)
	}
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: path},
		&clientcmd.ConfigOverrides{CurrentContext: baseContext},
	).ClientConfig()
	if err != nil {
		return nil, err
	}
	// Everything in the process shares this loopback; do not throttle it.
	cfg.QPS, cfg.Burst = -1, -1
	return cfg, nil
}
