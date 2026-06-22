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

package transformer

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/pkg/errors"

	"go.platform-mesh.io/apis/ui/v1alpha1"
	"go.platform-mesh.io/extension-manager-operator/pkg/validation"
)

type UrlSuffixTransformer struct{}

func (*UrlSuffixTransformer) Transform(contentConfiguration *validation.ContentConfiguration, instance *v1alpha1.ContentConfiguration) error {
	if instance.Spec.RemoteConfiguration != nil {
		parsedUrl, err := url.Parse(instance.Spec.RemoteConfiguration.URL)
		if err != nil { // coverage-ignore
			return errors.Wrap(err, "failed to parse URL")
		}
		domain := fmt.Sprintf("%s://%s", parsedUrl.Scheme, parsedUrl.Host)

		for i := range contentConfiguration.LuigiConfigFragment.Data.Nodes {
			transformNode(&contentConfiguration.LuigiConfigFragment.Data.Nodes[i], domain)
		}
		return nil
	}
	return nil
}

func transformNode(node *validation.Node, domain string) {
	if node.UrlSuffix != "" {
		domain = strings.TrimRight(domain, "/")
		urlSuffix := strings.TrimLeft(node.UrlSuffix, "/")
		url := fmt.Sprintf("%s/%s", domain, urlSuffix)
		node.Url = url
		node.UrlSuffix = ""
	}
	for i := range node.Children {
		transformNode(&node.Children[i], domain)
	}
}
