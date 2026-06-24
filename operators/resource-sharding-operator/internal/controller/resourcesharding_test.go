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

//nolint:goconst
package controller

import (
	"fmt"
	"time"

	pmshardingv1alpha1 "go.platform-mesh.io/apis/sharding/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func (s *ResourceShardingSuite) TestHappyPath() {
	rs := &pmshardingv1alpha1.ResourceSharding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-happy-path",
		},
		Spec: pmshardingv1alpha1.ResourceShardingSpec{
			Target: pmshardingv1alpha1.TargetResource{
				Group:    "",
				Version:  "v1",
				Resource: "configmaps",
			},
			ShardLabelKey: "test.sharding.io/shard",
			Shards: []pmshardingv1alpha1.ShardRef{
				{Name: "shard-a"},
				{Name: "shard-b"},
				{Name: "shard-c"},
			},
			Rebalance: pmshardingv1alpha1.RebalanceConfig{
				Interval: metav1.Duration{Duration: 2 * time.Second},
			},
		},
	}

	s.Require().NoError(s.k8sClient.Create(s.ctx, rs))
	defer func() {
		_ = s.k8sClient.Delete(s.ctx, rs)
	}()

	// Wait for Ready condition
	s.Eventually(func() bool {
		var fetched pmshardingv1alpha1.ResourceSharding
		if err := s.k8sClient.Get(s.ctx, types.NamespacedName{Name: rs.Name}, &fetched); err != nil {
			return false
		}
		for _, c := range fetched.Status.Conditions {
			if c.Type == "Ready" && c.Status == metav1.ConditionTrue {
				return true
			}
		}
		return false
	}, testTimeout, testInterval, "ResourceSharding should become Ready")

	// Create unlabeled configmaps in a test namespace
	ns := &unstructured.Unstructured{}
	ns.SetGroupVersionKind(schema.GroupVersionKind{Version: "v1", Kind: "Namespace"})
	ns.SetName("test-happy-path")
	_ = s.k8sClient.Create(s.ctx, ns)
	defer func() {
		_ = s.k8sClient.Delete(s.ctx, ns)
	}()

	for i := range 9 {
		cm := &unstructured.Unstructured{}
		cm.SetGroupVersionKind(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"})
		cm.SetName(fmt.Sprintf("test-cm-%d", i))
		cm.SetNamespace("test-happy-path")
		cm.Object["data"] = map[string]any{"key": "value"}
		s.Require().NoError(s.k8sClient.Create(s.ctx, cm))
	}
	defer func() {
		for i := range 9 {
			cm := &unstructured.Unstructured{}
			cm.SetGroupVersionKind(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"})
			cm.SetName(fmt.Sprintf("test-cm-%d", i))
			cm.SetNamespace("test-happy-path")
			_ = s.k8sClient.Delete(s.ctx, cm)
		}
	}()

	// Wait for all 9 configmaps to get shard labels
	s.Eventually(func() bool {
		labeled := 0
		for _, shard := range []string{"shard-a", "shard-b", "shard-c"} {
			list := &unstructured.UnstructuredList{}
			list.SetGroupVersionKind(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"})
			if err := s.k8sClient.List(s.ctx, list,
				ctrlruntimeclient.InNamespace("test-happy-path"),
				ctrlruntimeclient.MatchingLabels{"test.sharding.io/shard": shard}); err != nil {
				return false
			}
			labeled += len(list.Items)
		}
		return labeled == 9
	}, testTimeout, testInterval, "All 9 configmaps should have shard labels")

	// Verify distribution is roughly even (each shard has 2-4)
	for _, shard := range []string{"shard-a", "shard-b", "shard-c"} {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"})
		err := s.k8sClient.List(s.ctx, list,
			ctrlruntimeclient.InNamespace("test-happy-path"),
			ctrlruntimeclient.MatchingLabels{"test.sharding.io/shard": shard})
		s.Require().NoError(err)
		s.GreaterOrEqual(len(list.Items), 2, "shard %s should have at least 2", shard)
		s.LessOrEqual(len(list.Items), 4, "shard %s should have at most 4", shard)
	}
}

func (s *ResourceShardingSuite) TestSelfHealing() {
	rs := &pmshardingv1alpha1.ResourceSharding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-self-healing",
		},
		Spec: pmshardingv1alpha1.ResourceShardingSpec{
			Target: pmshardingv1alpha1.TargetResource{
				Group:    "",
				Version:  "v1",
				Resource: "configmaps",
			},
			ShardLabelKey: "test.healing.io/shard",
			Shards: []pmshardingv1alpha1.ShardRef{
				{Name: "shard-x"},
				{Name: "shard-y"},
			},
			Rebalance: pmshardingv1alpha1.RebalanceConfig{
				Interval: metav1.Duration{Duration: 2 * time.Second},
			},
		},
	}

	s.Require().NoError(s.k8sClient.Create(s.ctx, rs))
	defer func() {
		_ = s.k8sClient.Delete(s.ctx, rs)
	}()

	// Create a configmap
	cm := &unstructured.Unstructured{}
	cm.SetGroupVersionKind(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"})
	cm.SetName("test-heal-cm")
	cm.SetNamespace(corev1.NamespaceDefault)
	cm.Object["data"] = map[string]any{"key": "value"}
	s.Require().NoError(s.k8sClient.Create(s.ctx, cm))
	defer func() {
		_ = s.k8sClient.Delete(s.ctx, cm)
	}()

	// Wait for label assignment
	s.Eventually(func() bool {
		var fetched unstructured.Unstructured
		fetched.SetGroupVersionKind(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"})
		if err := s.k8sClient.Get(s.ctx, types.NamespacedName{Name: "test-heal-cm", Namespace: corev1.NamespaceDefault}, &fetched); err != nil {
			return false
		}
		_, exists := fetched.GetLabels()["test.healing.io/shard"]
		return exists
	}, testTimeout, testInterval, "ConfigMap should get shard label")

	// Remove the label
	var fetched unstructured.Unstructured
	fetched.SetGroupVersionKind(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"})
	s.Require().NoError(s.k8sClient.Get(s.ctx, types.NamespacedName{Name: "test-heal-cm", Namespace: corev1.NamespaceDefault}, &fetched))

	labels := fetched.GetLabels()
	delete(labels, "test.healing.io/shard")
	fetched.SetLabels(labels)
	s.Require().NoError(s.k8sClient.Update(s.ctx, &fetched))

	// Verify label gets reassigned
	s.Eventually(func() bool {
		var refetched unstructured.Unstructured
		refetched.SetGroupVersionKind(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"})
		if err := s.k8sClient.Get(s.ctx, types.NamespacedName{Name: "test-heal-cm", Namespace: corev1.NamespaceDefault}, &refetched); err != nil {
			return false
		}
		_, exists := refetched.GetLabels()["test.healing.io/shard"]
		return exists
	}, testTimeout, testInterval, "ConfigMap should get shard label reassigned after removal")
}

func (s *ResourceShardingSuite) TestUniquenessValidation() {
	rs1 := &pmshardingv1alpha1.ResourceSharding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-unique-1",
		},
		Spec: pmshardingv1alpha1.ResourceShardingSpec{
			Target: pmshardingv1alpha1.TargetResource{
				Group:    "",
				Version:  "v1",
				Resource: "secrets",
			},
			ShardLabelKey: "test.unique.io/shard",
			Shards:        []pmshardingv1alpha1.ShardRef{{Name: "s1"}},
			Rebalance: pmshardingv1alpha1.RebalanceConfig{
				Interval: metav1.Duration{Duration: 5 * time.Minute},
			},
		},
	}
	rs2 := &pmshardingv1alpha1.ResourceSharding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-unique-2",
		},
		Spec: pmshardingv1alpha1.ResourceShardingSpec{
			Target: pmshardingv1alpha1.TargetResource{
				Group:    "",
				Version:  "v1",
				Resource: "secrets",
			},
			ShardLabelKey: "test.unique.io/shard",
			Shards:        []pmshardingv1alpha1.ShardRef{{Name: "s2"}},
			Rebalance: pmshardingv1alpha1.RebalanceConfig{
				Interval: metav1.Duration{Duration: 5 * time.Minute},
			},
		},
	}

	s.Require().NoError(s.k8sClient.Create(s.ctx, rs1))
	s.Require().NoError(s.k8sClient.Create(s.ctx, rs2))
	defer func() {
		_ = s.k8sClient.Delete(s.ctx, rs1)
		_ = s.k8sClient.Delete(s.ctx, rs2)
	}()

	// Second ResourceSharding should get Conflict condition
	s.Eventually(func() bool {
		var fetched pmshardingv1alpha1.ResourceSharding
		if err := s.k8sClient.Get(s.ctx, types.NamespacedName{Name: rs2.Name}, &fetched); err != nil {
			return false
		}
		for _, c := range fetched.Status.Conditions {
			if c.Type == "Conflict" && c.Status == metav1.ConditionTrue {
				return true
			}
		}
		return false
	}, testTimeout, testInterval, "Second ResourceSharding should have Conflict condition")
}

func (s *ResourceShardingSuite) TestTargetNotFound() {
	rs := &pmshardingv1alpha1.ResourceSharding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-not-found",
		},
		Spec: pmshardingv1alpha1.ResourceShardingSpec{
			Target: pmshardingv1alpha1.TargetResource{
				Group:    "nonexistent.example.io",
				Version:  "v1",
				Resource: "fakes",
			},
			ShardLabelKey: "test.notfound.io/shard",
			Shards:        []pmshardingv1alpha1.ShardRef{{Name: "s1"}},
			Rebalance: pmshardingv1alpha1.RebalanceConfig{
				Interval: metav1.Duration{Duration: 5 * time.Minute},
			},
		},
	}

	s.Require().NoError(s.k8sClient.Create(s.ctx, rs))
	defer func() {
		_ = s.k8sClient.Delete(s.ctx, rs)
	}()

	s.Eventually(func() bool {
		var fetched pmshardingv1alpha1.ResourceSharding
		if err := s.k8sClient.Get(s.ctx, types.NamespacedName{Name: rs.Name}, &fetched); err != nil {
			return false
		}
		for _, c := range fetched.Status.Conditions {
			if c.Type == "TargetNotFound" && c.Status == metav1.ConditionTrue {
				return true
			}
		}
		return false
	}, testTimeout, testInterval, "ResourceSharding should have TargetNotFound condition")
}
