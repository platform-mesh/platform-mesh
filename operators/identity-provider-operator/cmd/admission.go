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
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	identityadmission "go.platform-mesh.io/apis/identity/admission"
	pmidentityv1alpha1 "go.platform-mesh.io/apis/identity/v1alpha1"
	"go.platform-mesh.io/golang-commons/idptransport"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	webhookadmission "sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/kcp-dev/logicalcluster/v3"
)

type admissionOptions struct {
	bindAddress, kubeconfig, classWorkspace, className, namespace string
	certFile, keyFile, clientCA, providerClientSecret             string
	workloads                                                     []string
	timeout                                                       time.Duration
}

func NewCommand() *cobra.Command {
	root := &cobra.Command{Use: "identity-provider-operator", SilenceUsage: true}
	root.AddCommand(newOrganizationPublicationCommand())
	options := admissionOptions{}
	cmd := &cobra.Command{
		Use: "admission", Short: "Serve the staged provider-neutral identity admission endpoint",
		RunE: func(cmd *cobra.Command, _ []string) error { return options.run(cmd.Context()) },
	}
	flags := cmd.Flags()
	flags.StringVar(&options.bindAddress, "bind-address", ":9443", "Mutual TLS admission listen address")
	flags.StringVar(&options.kubeconfig, "kcp-kubeconfig", "/api-kubeconfig/kubeconfig", "KCP service identity kubeconfig")
	flags.StringVar(&options.classWorkspace, "class-workspace", "root:platform-mesh-system", "Installation-owned provider-class workspace")
	flags.StringVar(&options.className, "provider-class", "default", "Installation-selected provider class")
	flags.StringVar(&options.namespace, "runtime-namespace", "platform-mesh-system", "Private runtime Secret namespace")
	flags.StringVar(&options.certFile, "tls-cert-file", "", "Admission serving certificate")
	flags.StringVar(&options.keyFile, "tls-key-file", "", "Admission serving private key")
	flags.StringVar(&options.clientCA, "client-ca-file", "", "Trusted KCP admission caller CA")
	flags.StringSliceVar(&options.workloads, "allowed-workloads", nil, "Exact SPIFFE identities permitted to submit AdmissionReviews")
	flags.StringVar(&options.providerClientSecret, "provider-client-secret", "identity-provider-validator-client", "Runtime Secret containing tls.crt and tls.key for provider validator calls")
	flags.DurationVar(&options.timeout, "request-timeout", 10*time.Second, "Maximum admission, provider and KCP request duration")
	root.AddCommand(cmd)
	return root
}

func (o admissionOptions) run(ctx context.Context) error {
	if o.timeout <= 0 || o.className == "" || o.namespace == "" || o.providerClientSecret == "" || len(o.workloads) == 0 {
		return errors.New("provider class, runtime namespace, provider client Secret, positive timeout and caller allowlist are required")
	}
	for _, workload := range o.workloads {
		uri, err := url.Parse(workload)
		if err != nil || uri.Scheme != "spiffe" || uri.Host == "" || uri.User != nil || strings.ContainsAny(workload, "?#") {
			return errors.New("admission callers must be exact SPIFFE workload identities")
		}
	}
	certificate, err := tls.LoadX509KeyPair(o.certFile, o.keyFile)
	if err != nil {
		return err
	}
	callers, err := idptransport.CertPool(o.clientCA)
	if err != nil {
		return err
	}
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		return err
	}
	if err := pmidentityv1alpha1.AddToScheme(scheme); err != nil {
		return err
	}
	kcpConfig, err := clientcmd.BuildConfigFromFlags("", o.kubeconfig)
	if err != nil {
		return err
	}
	classConfig, err := workspaceConfig(kcpConfig, o.classWorkspace, o.timeout)
	if err != nil {
		return err
	}
	classes, err := ctrlruntimeclient.New(classConfig, ctrlruntimeclient.Options{Scheme: scheme})
	if err != nil {
		return err
	}
	runtimeConfig, err := ctrl.GetConfig()
	if err != nil {
		return err
	}
	runtimeConfig.Timeout = o.timeout
	private, err := ctrlruntimeclient.New(runtimeConfig, ctrlruntimeclient.Options{Scheme: scheme})
	if err != nil {
		return err
	}
	handler := &identityadmission.Handler{
		Classes: classes, ActiveClass: o.className,
		Privileged: func(_ context.Context, user authenticationv1.UserInfo) (bool, error) {
			return slices.Contains(user.Groups, "system:masters") || slices.Contains(user.Groups, "system:kcp:admin"), nil
		},
		Dispatcher: &identityadmission.HTTPDispatcher{
			Timeout: o.timeout,
			ClientForClass: func(ctx context.Context, class *pmidentityv1alpha1.IdentityProviderClass) (*http.Client, error) {
				return o.providerClient(ctx, private, class)
			},
		},
	}
	webhook := &webhookadmission.Webhook{Handler: handler}
	mux := http.NewServeMux()
	mux.HandleFunc("/identity-admission/v1/admit", func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "identity admission requires POST", http.StatusMethodNotAllowed)
			return
		}
		req.Body = http.MaxBytesReader(w, req.Body, 2<<20)
		webhook.ServeHTTP(w, req)
	})
	return idptransport.Serve(ctx, o.bindAddress, o.timeout, &tls.Config{
		MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}, ClientCAs: callers, ClientAuth: tls.RequireAndVerifyClientCert,
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.VerifiedChains) == 0 || len(state.VerifiedChains[0]) == 0 || len(state.VerifiedChains[0][0].URIs) != 1 ||
				!slices.Contains(o.workloads, state.VerifiedChains[0][0].URIs[0].String()) {
				return errors.New("identity admission caller is not authorized")
			}
			return nil
		},
	}, mux)
}

func workspaceConfig(config *rest.Config, workspace string, timeout time.Duration) (*rest.Config, error) {
	if config == nil || !logicalcluster.NewPath(workspace).IsValid() || workspace == "*" || timeout <= 0 {
		return nil, errors.New("trusted KCP workspace and positive request timeout are required")
	}
	host, err := url.Parse(config.Host)
	if err != nil || host.Scheme != "https" || host.Hostname() == "" || host.User != nil || host.RawQuery != "" || host.Fragment != "" {
		return nil, errors.New("KCP must use an administrator-configured HTTPS endpoint")
	}
	host.Path, host.RawPath = logicalcluster.NewPath(workspace).RequestPath(), ""
	scoped := rest.CopyConfig(config)
	scoped.Host, scoped.Timeout = host.String(), timeout
	return scoped, nil
}

func (o admissionOptions) providerClient(ctx context.Context, private ctrlruntimeclient.Reader, class *pmidentityv1alpha1.IdentityProviderClass) (*http.Client, error) {
	if private == nil || o.namespace == "" || o.providerClientSecret == "" || o.timeout <= 0 ||
		class == nil || class.Spec.Validation == nil {
		return nil, errors.New("trusted runtime namespace, provider client identity, validation endpoint and positive timeout are required")
	}
	certificate := &corev1.Secret{}
	if err := private.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: o.namespace, Name: o.providerClientSecret}, certificate); err != nil {
		return nil, err
	}
	workload, err := tls.X509KeyPair(certificate.Data["tls.crt"], certificate.Data["tls.key"])
	if err != nil {
		return nil, err
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, err
	}
	if class.Spec.Validation.TLSSecretRef != nil {
		ca := &corev1.Secret{}
		if err := private.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: o.namespace, Name: class.Spec.Validation.TLSSecretRef.Name}, ca); err != nil {
			return nil, err
		}
		roots = x509.NewCertPool()
		if !roots.AppendCertsFromPEM(ca.Data["ca.crt"]) {
			return nil, errors.New("provider validator CA Secret contains no certificates")
		}
	}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment, DialContext: (&net.Dialer{Timeout: o.timeout, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout: o.timeout, ResponseHeaderTimeout: o.timeout, IdleConnTimeout: 90 * time.Second,
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, Certificates: []tls.Certificate{workload}},
	}
	return &http.Client{Timeout: o.timeout, Transport: transport}, nil
}
