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

package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/kcp-migration-operator/internal/config"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func TestParseAPIVersion(t *testing.T) {
	tests := []struct {
		name       string
		apiVersion string
		want       schema.GroupVersion
	}{
		{
			name:       "core API with version only",
			apiVersion: "v1",
			want:       schema.GroupVersion{Group: "", Version: "v1"},
		},
		{
			name:       "apps API group",
			apiVersion: "apps/v1",
			want:       schema.GroupVersion{Group: "apps", Version: "v1"},
		},
		{
			name:       "custom API group with alpha version",
			apiVersion: "apps.example.com/v1alpha1",
			want:       schema.GroupVersion{Group: "apps.example.com", Version: "v1alpha1"},
		},
		{
			name:       "networking API group",
			apiVersion: "networking.k8s.io/v1",
			want:       schema.GroupVersion{Group: "networking.k8s.io", Version: "v1"},
		},
		{
			name:       "batch API group with beta version",
			apiVersion: "batch/v1beta1",
			want:       schema.GroupVersion{Group: "batch", Version: "v1beta1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseAPIVersion(tt.apiVersion)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNewSyncController(t *testing.T) {
	tests := []struct {
		name             string
		sourceAPIVersion string
		sourceKind       string
		wantGVK          schema.GroupVersionKind
	}{
		{
			name:             "core ConfigMap",
			sourceAPIVersion: "v1",
			sourceKind:       "ConfigMap",
			wantGVK:          schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"},
		},
		{
			name:             "apps Deployment",
			sourceAPIVersion: "apps/v1",
			sourceKind:       "Deployment",
			wantGVK:          schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
		},
		{
			name:             "custom resource",
			sourceAPIVersion: "migration.platform-mesh.io/v1alpha1",
			sourceKind:       "KCPMigration",
			wantGVK:          schema.GroupVersionKind{Group: "migration.platform-mesh.io", Version: "v1alpha1", Kind: "KCPMigration"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.SyncConfig{
				Source: config.SourceConfig{
					APIVersion: tt.sourceAPIVersion,
					Kind:       tt.sourceKind,
				},
			}
			controller := NewSyncController(nil, nil, cfg, nil)
			assert.Equal(t, tt.wantGVK, controller.gvk)
		})
	}
}

func TestBuildLabelSelectorPredicate(t *testing.T) {
	logCfg := logger.DefaultConfig()
	logCfg.Level = "debug"
	log, err := logger.New(logCfg)
	require.NoError(t, err)

	tests := []struct {
		name           string
		labelSelectors []string
		objLabels      map[string]string
		expectMatch    bool
		expectError    bool
	}{
		{
			name:           "single selector matches",
			labelSelectors: []string{"app=myapp"},
			objLabels:      map[string]string{"app": "myapp"},
			expectMatch:    true,
		},
		{
			name:           "single selector does not match",
			labelSelectors: []string{"app=myapp"},
			objLabels:      map[string]string{"app": "otherapp"},
			expectMatch:    false,
		},
		{
			name:           "multiple selectors all match",
			labelSelectors: []string{"app=myapp", "env=prod"},
			objLabels:      map[string]string{"app": "myapp", "env": "prod"},
			expectMatch:    true,
		},
		{
			name:           "multiple selectors one does not match",
			labelSelectors: []string{"app=myapp", "env=prod"},
			objLabels:      map[string]string{"app": "myapp", "env": "dev"},
			expectMatch:    false,
		},
		{
			name:           "selector with existence check",
			labelSelectors: []string{"app"},
			objLabels:      map[string]string{"app": "myapp"},
			expectMatch:    true,
		},
		{
			name:           "selector with existence check - label missing",
			labelSelectors: []string{"app"},
			objLabels:      map[string]string{"other": "value"},
			expectMatch:    false,
		},
		{
			name:           "selector with not-in operator",
			labelSelectors: []string{"env notin (staging,dev)"},
			objLabels:      map[string]string{"env": "prod"},
			expectMatch:    true,
		},
		{
			name:           "selector with not-in operator - should not match",
			labelSelectors: []string{"env notin (staging,dev)"},
			objLabels:      map[string]string{"env": "dev"},
			expectMatch:    false,
		},
		{
			name:           "selector with in operator",
			labelSelectors: []string{"env in (prod,staging)"},
			objLabels:      map[string]string{"env": "prod"},
			expectMatch:    true,
		},
		{
			name:           "empty labels with selector",
			labelSelectors: []string{"app=myapp"},
			objLabels:      map[string]string{},
			expectMatch:    false,
		},
		{
			name:           "invalid selector syntax",
			labelSelectors: []string{"invalid[selector"},
			objLabels:      map[string]string{"app": "myapp"},
			expectError:    true,
		},
		{
			name:           "extra labels on object still matches",
			labelSelectors: []string{"app=myapp"},
			objLabels:      map[string]string{"app": "myapp", "env": "prod", "version": "v1"},
			expectMatch:    true,
		},
		{
			name:           "not equals selector matches",
			labelSelectors: []string{"env!=dev"},
			objLabels:      map[string]string{"env": "prod"},
			expectMatch:    true,
		},
		{
			name:           "not equals selector does not match",
			labelSelectors: []string{"env!=dev"},
			objLabels:      map[string]string{"env": "dev"},
			expectMatch:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.SyncConfig{
				Source: config.SourceConfig{
					APIVersion:     "v1",
					Kind:           "ConfigMap",
					LabelSelectors: tt.labelSelectors,
				},
			}
			controller := NewSyncController(nil, log, cfg, nil)

			pred, err := controller.buildLabelSelectorPredicate()

			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, pred)

			obj := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test",
					Labels: tt.objLabels,
				},
			}

			createEvent := event.CreateEvent{Object: obj}
			result := pred.Create(createEvent)
			assert.Equal(t, tt.expectMatch, result)

			updateEvent := event.UpdateEvent{ObjectNew: obj, ObjectOld: obj}
			updateResult := pred.Update(updateEvent)
			assert.Equal(t, tt.expectMatch, updateResult)

			deleteEvent := event.DeleteEvent{Object: obj}
			deleteResult := pred.Delete(deleteEvent)
			assert.Equal(t, tt.expectMatch, deleteResult)
		})
	}
}

func TestLabelSelectorParsing(t *testing.T) {
	tests := []struct {
		name        string
		selector    string
		expectError bool
	}{
		{
			name:        "simple equality",
			selector:    "app=myapp",
			expectError: false,
		},
		{
			name:        "double equality",
			selector:    "app==myapp",
			expectError: false,
		},
		{
			name:        "not equals",
			selector:    "app!=myapp",
			expectError: false,
		},
		{
			name:        "in operator",
			selector:    "env in (prod,staging)",
			expectError: false,
		},
		{
			name:        "notin operator",
			selector:    "env notin (dev,test)",
			expectError: false,
		},
		{
			name:        "existence",
			selector:    "app",
			expectError: false,
		},
		{
			name:        "non-existence",
			selector:    "!app",
			expectError: false,
		},
		{
			name:        "invalid bracket",
			selector:    "app[invalid",
			expectError: true,
		},
		{
			name:        "combined selectors",
			selector:    "app=myapp,env=prod",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := labels.Parse(tt.selector)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
