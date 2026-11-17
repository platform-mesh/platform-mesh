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
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"go.platform-mesh.io/golang-commons/idptransport"
	"go.platform-mesh.io/pm-keycloak-operator/pkg/identityprovider"

	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	webhookadmission "sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type providerValidationServer struct {
	bindAddress, certFile, keyFile, clientCA string
	workloads                                []string
	timeout                                  time.Duration
	handler                                  *identityprovider.AdmissionHandler
}

func (s *providerValidationServer) NeedLeaderElection() bool { return false }

func (s *providerValidationServer) Start(ctx context.Context) error {
	if len(s.workloads) == 0 {
		return errors.New("provider validation requires an explicit SPIFFE workload allowlist")
	}
	for _, workload := range s.workloads {
		uri, err := url.Parse(workload)
		if err != nil || uri.Scheme != "spiffe" || uri.Host == "" || uri.User != nil || strings.ContainsAny(workload, "?#") {
			return errors.New("provider validation callers must be exact SPIFFE workload identities")
		}
	}
	certificate, err := tls.LoadX509KeyPair(s.certFile, s.keyFile)
	if err != nil {
		return err
	}
	roots, err := idptransport.CertPool(s.clientCA)
	if err != nil {
		return err
	}
	webhook := &webhookadmission.Webhook{Handler: s.handler}
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/identity-validation/v1/admit" {
			http.NotFound(w, req)
			return
		}
		if req.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "provider validation requires POST", http.StatusMethodNotAllowed)
			return
		}
		if req.TLS == nil || len(req.TLS.VerifiedChains) == 0 || len(req.TLS.PeerCertificates) == 0 ||
			len(req.TLS.PeerCertificates[0].URIs) != 1 || !slices.Contains(s.workloads, req.TLS.PeerCertificates[0].URIs[0].String()) {
			http.Error(w, "provider validation caller is not authorized", http.StatusForbidden)
			return
		}
		req.Body = http.MaxBytesReader(w, req.Body, 2<<20)
		webhook.ServeHTTP(w, req)
	})
	return idptransport.Serve(ctx, s.bindAddress, s.timeout, &tls.Config{
		MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate},
		ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots,
	}, handler)
}

func (s *providerValidationServer) configure(classes, private ctrlruntimeclient.Reader, className, namespace string, timeout time.Duration) {
	s.timeout = timeout
	s.handler = &identityprovider.AdmissionHandler{
		Classes: classes, Private: private, ClassName: className, RuntimeNamespace: namespace, Timeout: timeout,
	}
}
