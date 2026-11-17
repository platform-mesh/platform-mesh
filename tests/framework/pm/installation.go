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

package pm

import (
	"context"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/url"
	"os"

	"go.platform-mesh.io/platform-mesh/tests/framework/keycloak"

	"k8s.io/client-go/rest"
)

// An Installation is a deployed Platform Mesh, as a test reaches it. The
// functions of this file's siblings (Kube, KCP, Keycloak) build its parts
// from the environment of an e2e test; a test that deploys Platform Mesh
// itself, like tests/upgrade, implements it for what it deployed. Helpers
// that need more than one part take the Installation.
//
// Every method is asked again whenever a part is needed, so an
// implementation can follow an installation that changes under the test.
type Installation interface {
	// KCP is the kcp admin config of the installation, without a
	// workspace path.
	KCP(ctx context.Context) (*rest.Config, error)
	// Keycloak is the admin client of the Keycloak of the installation.
	Keycloak(ctx context.Context) (keycloak.Client, error)
	// Kube is the admin config of the cluster Platform Mesh is installed
	// into, where its PlatformMesh object and its secrets are.
	Kube(ctx context.Context) (*rest.Config, error)
	// Portal is the address of the portal, for example
	// https://portal.example.com:8443, and an HTTP client that trusts its
	// certificate. The services of an organization are served under it,
	// on the subdomain of the organization: see OrganizationURL.
	Portal(ctx context.Context) (string, *http.Client, error)
}

// Roots is the pool of the CA certificates in the PEM file, for a client
// that checks the certificates of an installation against them.
func Roots(file string) (*x509.CertPool, error) {
	pem, err := os.ReadFile(file) //nolint:gosec // the path is the caller's
	if err != nil {
		return nil, fmt.Errorf("reading the CA certificates: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("%s holds no certificate", file)
	}
	return roots, nil
}

// OrganizationURL is the address of a service of the organization: the path
// under the address of the portal, on the subdomain of the organization.
func OrganizationURL(portal, org, path string) (string, error) {
	target, err := url.Parse(portal)
	if err != nil {
		return "", fmt.Errorf("the address of the portal: %w", err)
	}
	target.Host = org + "." + target.Host
	target.Path = path
	return target.String(), nil
}
