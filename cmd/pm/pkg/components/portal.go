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

package components

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"

	"go.platform-mesh.io/platform-mesh/cmd/pm/pkg/docker"

	"k8s.io/klog/v2"
)

// PortalBaseDomain is the host the portal is opened on. The portal takes
// the organization from the first label of the host, and browsers resolve
// every name under localhost to the loopback address.
const PortalBaseDomain = "localhost"

// Portal is the portal UI. It is a Node.js server, so it cannot be linked
// into the process: it runs as a container on the host network, started
// with the process and stopped with it. Browsers reach it through Ingress. It logs in at the mock OIDC issuer
// and reads the workspaces through the GraphQL gateway.
//
// Without docker, or without the mock issuer, the portal is skipped and the
// rest of the process runs on.
func Portal() Component {
	return Component{
		Name: "portal",
		Run: func(ctx context.Context, env Env) error {
			log := klog.FromContext(ctx).WithName("portal")
			skip := func(err error) error {
				log.Error(err, "not running the portal")
				<-ctx.Done()
				return nil
			}
			if env.OIDC == nil {
				return skip(errors.New("the portal needs the mock OIDC issuer (--mock-oidc)"))
			}
			if err := docker.Available(ctx); err != nil {
				return skip(err)
			}

			const kubeconfig, issuerCA = "/pm/kubeconfig", "/pm/oidc-ca.crt"
			port := strconv.Itoa(env.Ports.Portal)
			container := docker.Container{
				// One per portal port, so that two processes do not take each
				// other's container.
				Name:  fmt.Sprintf("pm-portal-%d", env.PortalPort),
				Image: env.PortalImage,
				// The mounted files belong to whoever runs pm.
				User:        fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
				HostNetwork: true,
				Mounts: []string{
					env.Kubeconfig + ":" + kubeconfig + ":ro",
					env.OIDC.CAFile + ":" + issuerCA + ":ro",
				},
				Env: map[string]string{
					"PORT": port,
					// Where the browser sees it: Ingress.
					"FRONTEND_PORT": strconv.Itoa(env.PortalPort),
					// The org on the bare base domain: the welcome page,
					// which lists the organizations.
					"BASE_DOMAINS_DEFAULT":   PortalBaseDomain,
					"OIDC_CLIENT_ID_DEFAULT": "welcome",

					"DISCOVERY_ENDPOINT":        env.OIDC.IssuerURL + "/.well-known/openid-configuration",
					"OIDC_STATIC_CLIENT_ID":     env.OIDC.ClientID,
					"OIDC_STATIC_CLIENT_SECRET": env.OIDC.ClientSecret,
					"OIDC_SCOPE":                "openid email groups profile",
					"NODE_EXTRA_CA_CERTS":       issuerCA,

					// Only the server of the kubeconfig is used, and its CA.
					"KUBECONFIG_KCP": kubeconfig,
					"KCP_URL":        env.Config.Host,
					// What the pages call, through Ingress. The browser replaces
					// root:orgs:... with the workspace it shows.
					"OPENMFP_PORTAL_CONTEXT_CRD_GATEWAY_API_URL": PublicURL(env.PortalPort, "/gateway/api/clusters/root:orgs:${org-name}/graphql"),
					"OPENMFP_PORTAL_CONTEXT_IAM_SERVICE_API_URL": PublicURL(env.PortalPort, "/iam/graphql"),
				},
			}
			log.Info("starting the portal container", "image", container.Image, "url", PortalURL(env.PortalPort))
			if err := container.Run(ctx); err != nil {
				return skip(err)
			}
			return nil
		},
	}
}

// PortalURL is where the portal is opened.
func PortalURL(port int) string {
	return fmt.Sprintf("http://%s:%d", PortalBaseDomain, port)
}
