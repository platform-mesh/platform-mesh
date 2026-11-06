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

package suite

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	appsv1 "k8s.io/api/apps/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const envoyGatewayVersion = "v1.7.0"

func installEnvoyGateway(t *testing.T, c *Cluster) {
	t.Helper()
	helm(t, c, "upgrade", "--install", "envoy",
		"oci://registry-1.docker.io/envoyproxy/gateway-helm", "--version", envoyGatewayVersion,
		"--namespace", "envoy-gateway-system", "--create-namespace", "--wait", "--timeout", "5m")
	applyKustomize(t, c, base("bases", "envoy-gateway"))
	require.Eventually(t, func() bool {
		list := &appsv1.DeploymentList{}
		if err := c.Client.List(t.Context(), list,
			ctrlruntimeclient.InNamespace("envoy-gateway-system"),
			ctrlruntimeclient.MatchingLabels{"gateway.envoyproxy.io/owning-gateway-name": "eg"},
		); err != nil {
			return false
		}
		for i := range list.Items {
			d := &list.Items[i]
			if d.Status.AvailableReplicas > 0 && d.Status.AvailableReplicas == d.Status.Replicas {
				return true
			}
		}
		return false
	}, 5*time.Minute, 5*time.Second, "envoy gateway proxy deployment not available")
}

// nodeInternalIP returns the cluster node's IP on the shared kind docker network.
func nodeInternalIP(t *testing.T, c *Cluster) string {
	t.Helper()
	path := writeKubeconfig(t, c.Config)
	out, err := exec.Command("kubectl", "--kubeconfig", path, "get", "nodes", //nolint:gosec // test-controlled args
		"-o", `jsonpath={.items[0].status.addresses[?(@.type=="InternalIP")].address}`).Output()
	require.NoError(t, err)
	ip := strings.TrimSpace(string(out))
	require.NotEmpty(t, ip, "node InternalIP")
	return ip
}

// dashedIP renders an IP as an sslip.io label, e.g. 172.18.0.3 -> 172-18-0-3.
func dashedIP(ip string) string { return strings.ReplaceAll(ip, ".", "-") }

func helm(t *testing.T, c *Cluster, args ...string) {
	t.Helper()
	path := writeKubeconfig(t, c.Config)
	sh(t, "helm", append([]string{"--kubeconfig", path}, args...)...)
}
