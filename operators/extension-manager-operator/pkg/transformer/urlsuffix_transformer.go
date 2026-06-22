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
