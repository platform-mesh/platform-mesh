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

package broker

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/config"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
)

// newManager returns a multicluster manager configured to coexist with other
// managers in the same process.
func newManager(cfg *rest.Config, scheme *runtime.Scheme, provider multicluster.Provider) (mcmanager.Manager, error) {
	return mcmanager.New(cfg, provider, mcmanager.Options{
		Scheme:           scheme,
		PprofBindAddress: "0",
		Controller: config.Controller{
			SkipNameValidation: ptr.To(true),
		},
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
}
