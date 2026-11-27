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
	"errors"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// kubeconfigFor returns a kubeconfig with the credentials of cfg for the
// server at host. kcp renews its admin token on every start, so a
// kubeconfig is good for the run it was made in.
func kubeconfigFor(cfg *rest.Config, host string) ([]byte, error) {
	if cfg.BearerToken == "" {
		return nil, errors.New("the kcp admin config has no token")
	}
	kubeconfig := clientcmdapi.NewConfig()
	kubeconfig.Clusters["kcp"] = &clientcmdapi.Cluster{Server: host, CertificateAuthorityData: cfg.CAData, CertificateAuthority: cfg.CAFile}
	kubeconfig.AuthInfos["admin"] = &clientcmdapi.AuthInfo{Token: cfg.BearerToken}
	kubeconfig.Contexts["kcp"] = &clientcmdapi.Context{Cluster: "kcp", AuthInfo: "admin"}
	kubeconfig.CurrentContext = "kcp"
	return clientcmd.Write(*kubeconfig)
}
