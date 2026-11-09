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

// Package wait holds the conditions integration tests wait for.
package wait

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

const (
	// Timeout is the default for the helpers below.
	Timeout = 60 * time.Second
	// Tick is the default poll interval.
	Tick = 200 * time.Millisecond
)

// Eventually fails t unless cond returns true within timeout, polling every
// tick and logging the reason when it changes. Unlike require.Eventually,
// cond runs in the calling goroutine, so nothing logs to t after the test
// ended; when t's context is done, polling stops without touching t.
func Eventually(t testing.TB, cond func() (bool, string), timeout, tick time.Duration, msg string, args ...any) {
	t.Helper()
	ctx := t.Context()
	deadline := time.Now().Add(timeout)
	start := time.Now()
	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	var last string
	for {
		select {
		case <-ctx.Done():
			// The test is over; t must not be used any more.
			runtime.Goexit()
		default:
		}
		ok, reason := cond()
		if ok {
			if reason != "" && last != "" {
				t.Logf("Condition became true: %s", reason)
			}
			return
		}
		if reason != "" && reason != last && time.Since(start) > timeout/5 {
			last = reason
			t.Logf("Waiting for condition, but got: %s", reason)
		}
		if time.Now().After(deadline) {
			if reason != "" {
				t.Fatalf("%s: condition not met within %s, last: %s", fmt.Sprintf(msg, args...), timeout, reason)
			}
			t.Fatalf("%s: condition not met within %s", fmt.Sprintf(msg, args...), timeout)
		}
		select {
		case <-ctx.Done():
			runtime.Goexit()
		case <-ticker.C:
		}
	}
}

// Until is Eventually for a cond returning only bool.
func Until(t testing.TB, cond func() bool, timeout, tick time.Duration, msg string, args ...any) {
	t.Helper()
	Eventually(t, func() (bool, string) { return cond(), "" }, timeout, tick, msg, args...)
}

// WorkspaceReady waits until the Workspace name in cl's workspace is Ready.
func WorkspaceReady(t testing.TB, cl ctrlruntimeclient.Client, name string) {
	t.Helper()
	Eventually(t, func() (bool, string) {
		ws := &kcptenancyv1alpha1.Workspace{}
		if err := cl.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: name}, ws); err != nil {
			return false, fmt.Sprintf("getting workspace: %v", err)
		}
		if ws.Status.Phase != kcpcorev1alpha1.LogicalClusterPhaseReady {
			return false, fmt.Sprintf("phase %q:\n%s", ws.Status.Phase, toYAML(ws.Status))
		}
		return true, ""
	}, Timeout, Tick, "workspace %s should be Ready", name)
}

// APIBindingBound waits until the APIBinding name in cl's workspace is Bound.
func APIBindingBound(t testing.TB, cl ctrlruntimeclient.Client, name string) {
	t.Helper()
	Eventually(t, func() (bool, string) {
		binding := &kcpapisv1alpha2.APIBinding{}
		if err := cl.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: name}, binding); err != nil {
			return false, fmt.Sprintf("getting APIBinding: %v", err)
		}
		if binding.Status.Phase != kcpapisv1alpha2.APIBindingPhaseBound {
			return false, fmt.Sprintf("phase %q:\n%s", binding.Status.Phase, toYAML(binding.Status.Conditions))
		}
		return true, ""
	}, Timeout, Tick, "APIBinding %s should be Bound", name)
}

// CRDEstablished waits until the CustomResourceDefinition name in cl's
// workspace is Established.
func CRDEstablished(t testing.TB, cl ctrlruntimeclient.Client, name string) {
	t.Helper()
	Eventually(t, func() (bool, string) {
		crd := &apiextensionsv1.CustomResourceDefinition{}
		if err := cl.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: name}, crd); err != nil {
			return false, fmt.Sprintf("getting CRD: %v", err)
		}
		for _, cond := range crd.Status.Conditions {
			if cond.Type == apiextensionsv1.Established && cond.Status == apiextensionsv1.ConditionTrue {
				return true, ""
			}
		}
		return false, fmt.Sprintf("not established:\n%s", toYAML(crd.Status.Conditions))
	}, Timeout, Tick, "CRD %s should be Established", name)
}

// EndpointSliceServed waits until the APIExportEndpointSlice name in cl's
// workspace lists an endpoint URL (kcp lists none for an unbound export).
func EndpointSliceServed(t testing.TB, cl ctrlruntimeclient.Client, name string) {
	t.Helper()
	Eventually(t, func() (bool, string) {
		slice := &kcpapisv1alpha1.APIExportEndpointSlice{}
		if err := cl.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: name}, slice); err != nil {
			return false, fmt.Sprintf("getting APIExportEndpointSlice: %v", err)
		}
		if len(slice.Status.APIExportEndpoints) == 0 || slice.Status.APIExportEndpoints[0].URL == "" {
			return false, fmt.Sprintf("no endpoint URLs:\n%s", toYAML(slice.Status))
		}
		return true, ""
	}, Timeout, Tick, "APIExportEndpointSlice %s should have an endpoint URL", name)
}

func toYAML(v any) string {
	data, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%+v", v)
	}
	return string(data)
}
