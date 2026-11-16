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

package cmd

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"

	pmidentityv1alpha1 "go.platform-mesh.io/apis/identity/v1alpha1"
	"go.platform-mesh.io/golang-commons/idpdirectory"
	"go.platform-mesh.io/golang-commons/idptransport"
	iclient "go.platform-mesh.io/pm-keycloak-operator/internal/client"
	"go.platform-mesh.io/pm-keycloak-operator/pkg/directory"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kcp-dev/logicalcluster/v3"
)

type directoryOptions struct {
	bindAddress, kubeconfig, providerClass, classWorkspace string
	baseURL, clientID, providerCA                          string
	certFile, keyFile, clientCA                            string
	workloads, clusters                                    []string
	allowAnyTenantCluster                                  bool
	timeout                                                time.Duration
}

func newDirectoryCommand() *cobra.Command {
	options := directoryOptions{}
	cmd := &cobra.Command{
		Use: "directory", Short: "Serve the staged identity directory over authenticated TLS",
		RunE: func(_ *cobra.Command, _ []string) error { return options.run(ctrl.SetupSignalHandler()) },
	}
	flags := cmd.Flags()
	flags.StringVar(&options.bindAddress, "bind-address", ":9444", "Directory TLS listen address")
	flags.StringVar(&options.kubeconfig, "kcp-kubeconfig", "/api-kubeconfig/kubeconfig", "KCP service identity kubeconfig")
	flags.StringVar(&options.providerClass, "provider-class", "default", "Trusted identity provider class")
	flags.StringVar(&options.classWorkspace, "class-workspace", "root:platform-mesh-system", "Administrator-owned provider-class workspace")
	flags.StringVar(&options.baseURL, "keycloak-base-url", "", "Keycloak HTTPS administration origin and optional path")
	flags.StringVar(&options.clientID, "keycloak-client-id", "pm-keycloak-operator", "Keycloak administration client; secret from KEYCLOAK_CLIENT_SECRET")
	flags.StringVar(&options.providerCA, "provider-ca-file", "", "Optional Keycloak-specific CA bundle")
	flags.StringVar(&options.certFile, "tls-cert-file", "", "Directory serving certificate file")
	flags.StringVar(&options.keyFile, "tls-key-file", "", "Directory serving private key file")
	flags.StringVar(&options.clientCA, "client-ca-file", "", "CA bundle for trusted directory callers")
	flags.StringSliceVar(&options.workloads, "allowed-workloads", nil, "Exact allowed SPIFFE caller identities")
	flags.StringSliceVar(&options.clusters, "allowed-tenant-clusters", nil, "Immutable KCP logical cluster IDs permitted for these callers")
	flags.BoolVar(&options.allowAnyTenantCluster, "allow-any-tenant-cluster", false, "Allow the configured workloads to query organization tenants in any KCP logical cluster")
	flags.DurationVar(&options.timeout, "request-timeout", 30*time.Second, "Maximum provider, KCP and directory request duration")
	return cmd
}

func (o directoryOptions) run(ctx context.Context) error {
	authorize, err := o.authorizer()
	if err != nil {
		return err
	}
	certificate, err := tls.LoadX509KeyPair(o.certFile, o.keyFile)
	if err != nil {
		return fmt.Errorf("load directory serving certificate: %w", err)
	}
	clientRoots, err := idptransport.CertPool(o.clientCA)
	if err != nil {
		return fmt.Errorf("load directory caller trust: %w", err)
	}
	providerClient, cleanup, err := o.providerClient(ctx)
	if err != nil {
		return err
	}
	defer cleanup()
	config, err := getKubeconfigFromPath(o.kubeconfig)
	if err != nil {
		return err
	}
	config.Timeout = o.timeout
	scheme := runtime.NewScheme()
	if err := pmidentityv1alpha1.AddToScheme(scheme); err != nil {
		return err
	}
	getter := iclient.NewConfigSchemeKCPClientGetter(config, scheme)
	classReader, err := getter.NewClientForLogicalCluster(ctx, o.classWorkspace)
	if err != nil {
		return fmt.Errorf("create directory class reader: %w", err)
	}
	resolver := &directory.KCPResolver{
		ClassName: o.providerClass, ClassReader: classReader,
		ReaderForCluster: func(ctx context.Context, cluster logicalcluster.Name) (ctrlruntimeclient.Reader, error) {
			return getter.NewClientForLogicalCluster(ctx, cluster.String())
		},
	}
	backend, err := directory.New(o.baseURL, providerClient, resolver)
	if err != nil {
		return err
	}
	handler, err := idpdirectory.NewHandler(backend, authorize, slog.Default())
	if err != nil {
		return err
	}
	return idptransport.Serve(ctx, o.bindAddress, o.timeout, &tls.Config{
		MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate},
		ClientCAs: clientRoots, ClientAuth: tls.RequireAndVerifyClientCert,
	}, handler)
}

func (o directoryOptions) authorizer() (idpdirectory.Authorizer, error) {
	if o.timeout <= 0 || o.providerClass == "" || o.classWorkspace == "" || len(o.workloads) == 0 ||
		(len(o.clusters) == 0 && !o.allowAnyTenantCluster) || (len(o.clusters) > 0 && o.allowAnyTenantCluster) {
		return nil, errors.New("positive request timeout, provider class/workspace, workload allowlist, and either tenant-cluster allowlist or allow-any-tenant-cluster are required")
	}
	for _, workload := range o.workloads {
		uri, err := url.Parse(workload)
		if err != nil || uri.Scheme != "spiffe" || uri.Host == "" || uri.User != nil ||
			strings.ContainsAny(workload, "?#") {
			return nil, errors.New("allowed workloads must be exact SPIFFE identities")
		}
	}
	for _, cluster := range o.clusters {
		if !logicalcluster.Name(cluster).IsValid() {
			return nil, errors.New("allowed tenant clusters must be immutable logical cluster IDs, not workspace paths")
		}
	}
	return func(_ context.Context, workload string, tenant idpdirectory.Tenant) error {
		if !slices.Contains(o.workloads, workload) ||
			(!o.allowAnyTenantCluster && !slices.Contains(o.clusters, tenant.LogicalCluster)) {
			return idpdirectory.ErrUnauthorized
		}
		return nil
	}, nil
}

func (o directoryOptions) providerClient(ctx context.Context) (*http.Client, func(), error) {
	endpoint, err := url.Parse(o.baseURL)
	if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" || endpoint.User != nil ||
		strings.ContainsAny(o.baseURL, "?#") {
		return nil, nil, errors.New("keycloak administration base URL must use HTTPS without userinfo, query or fragment")
	}
	secret := os.Getenv("KEYCLOAK_CLIENT_SECRET")
	if o.clientID == "" || secret == "" {
		return nil, nil, errors.New("keycloak client ID and KEYCLOAK_CLIENT_SECRET are required")
	}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment, TLSHandshakeTimeout: o.timeout, ResponseHeaderTimeout: o.timeout,
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
	}
	if o.providerCA != "" {
		transport.TLSClientConfig.RootCAs, err = idptransport.CertPool(o.providerCA)
		if err != nil {
			return nil, nil, fmt.Errorf("load keycloak provider trust: %w", err)
		}
	}
	bounded := &http.Client{Transport: transport, Timeout: o.timeout, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return errors.New("keycloak directory redirects are forbidden")
	}}
	config := clientcredentials.Config{
		ClientID: o.clientID, ClientSecret: secret,
		TokenURL: strings.TrimSuffix(o.baseURL, "/") + "/realms/master/protocol/openid-connect/token",
	}
	client := config.Client(context.WithValue(ctx, oauth2.HTTPClient, bounded))
	client.Timeout = o.timeout
	client.CheckRedirect = bounded.CheckRedirect
	return client, transport.CloseIdleConnections, nil
}
