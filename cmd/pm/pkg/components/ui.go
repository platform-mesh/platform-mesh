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
	"fmt"

	"go.platform-mesh.io/platform-mesh/cmd/pm/pkg/docker"

	"k8s.io/klog/v2"
)

// The micro frontends the portal embeds are static sites served by nginx on
// this port of their image.
const uiContainerPort = 8080

// IAMUI is the members and roles pages of the portal, under /ui/iam.
func IAMUI() Component {
	c := staticUI("iam-ui",
		func(env Env) string { return env.IAMUIImage },
		func(env Env) int { return env.Ports.IAMUI })
	// Its entries in the portal's navigation.
	c.Manifests = []string{
		"01-platform-mesh-system/contentconfiguration-main-iam-ui.yaml",
		"01-platform-mesh-system/contentconfiguration-user-overview.yaml",
	}
	return c
}

// MarketplaceUI is the marketplace pages of the portal, under
// /ui/marketplace.
func MarketplaceUI() Component {
	c := staticUI("marketplace-ui",
		func(env Env) string { return env.MarketplaceUIImage },
		func(env Env) int { return env.Ports.MarketplaceUI })
	// Its entries in the portal's navigation.
	c.Manifests = []string{
		"01-platform-mesh-system/contentconfiguration-main-marketplace.yaml",
		"01-platform-mesh-system/contentconfiguration-account-marketplace.yaml",
	}
	return c
}

// staticUI runs a micro frontend's image as a container, published on a
// localhost port that Ingress routes the frontend's path to. Without docker
// it is skipped and the rest of the process runs on.
func staticUI(name string, image func(Env) string, port func(Env) int) Component {
	return Component{
		Name: name,
		Run: func(ctx context.Context, env Env) error {
			log := klog.FromContext(ctx).WithName(name)
			container := docker.Container{
				Name:  fmt.Sprintf("pm-%s-%d", name, env.PortalPort),
				Image: image(env),
				Ports: []string{fmt.Sprintf("127.0.0.1:%d:%d", port(env), uiContainerPort)},
			}
			log.Info("starting container", "image", container.Image)
			if err := container.Run(ctx); err != nil {
				log.Error(err, "not running "+name)
				<-ctx.Done()
			}
			return nil
		},
	}
}
