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

package resource

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/platform-mesh-operator/pkg/subroutines"
	"go.platform-mesh.io/platform-mesh-operator/pkg/subroutines/mocks"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type ResourceTestSuite struct {
	suite.Suite
	subroutine *ResourceSubroutine
	clientMock *mocks.Client
}

func TestResourceTestSuite(t *testing.T) {
	suite.Run(t, new(ResourceTestSuite))
}

func (s *ResourceTestSuite) SetupTest() {
	s.clientMock = new(mocks.Client)
	s.clientMock.On("List", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	s.subroutine = NewResourceSubroutine(s.clientMock, nil, nil)
}

func (s *ResourceTestSuite) Test_applyReleaseWithValues() {
	ctx := s.T().Context()

	inst := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "delivery.ocm.software/v1alpha1",
			"kind":       "Resource",
			"metadata": map[string]any{
				"name":      "test-resource",
				"namespace": "default",
				"annotations": map[string]any{
					"artifact": "chart",
					"repo":     "oci",
				},
			},
			"status": map[string]any{
				"conditions": []any{
					map[string]any{
						"type":   "Ready",
						"status": "True",
					},
				},
				"resource": map[string]any{
					"version": "25.2.3",
					"access": map[string]any{
						"type":           "ociArtifact",
						"imageReference": "oci://oci-registry-docker-registry.registry.svc.cluster.local/platform-mesh/upstream-images/charts/keycloak:25.2.3@sha256:cb5be99827d7cfa107fc7ca06f5b2fb0ea486f3ffb0315baf2be1bb348f9db77",
					},
				},
			},
			"spec": map[string]any{},
		},
	}

	clientMock := new(mocks.Client)
	s.subroutine = NewResourceSubroutine(clientMock, nil, nil)

	clientMock.On("List", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	clientMock.On("Get", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	clientMock.EXPECT().Patch(mock.Anything, mock.MatchedBy(func(obj ctrlruntimeclient.Object) bool {
		unstr, ok := obj.(*unstructured.Unstructured)
		if !ok {
			return false
		}
		url, _, _ := unstructured.NestedString(unstr.Object, "spec", "url")
		return url == "oci://oci-registry-docker-registry.registry.svc.cluster.local/platform-mesh/upstream-images/charts/keycloak"
	}), mock.Anything, mock.Anything, mock.Anything).Return(nil)

	result, err := s.subroutine.Process(ctx, inst)
	s.Nil(err)
	s.NotNil(result)
}

func (s *ResourceTestSuite) Test_GetName() {
	s.Equal("ResourceSubroutine", s.subroutine.GetName())
}

func (s *ResourceTestSuite) Test_Finalize() {
	ctx := s.T().Context()
	inst := &unstructured.Unstructured{}
	result, err := s.subroutine.Finalize(ctx, inst)
	s.Nil(err)
	s.NotNil(result)
}

func (s *ResourceTestSuite) Test_Finalizers() {
	inst := &unstructured.Unstructured{}
	finalizers := s.subroutine.Finalizers(inst)
	s.Empty(finalizers)
}

func (s *ResourceTestSuite) Test_updateHelmReleaseWithImageTag() {
	tests := []struct {
		name                  string
		repo                  string
		artifact              string
		forAnnotation         string
		pathAnnotation        string
		versionPathAnnotation string
		resourceName          string
		resourceNs            string
		version               string
		versionPath           []string
		expectedName          string
		expectedNs            string
		expectedPath          []string
		expectedVersion       string
	}{
		{
			name:                  "helm image without annotations",
			repo:                  "helm",
			artifact:              "image",
			forAnnotation:         "",
			pathAnnotation:        "",
			versionPathAnnotation: "",
			resourceName:          "test-resource",
			resourceNs:            "default",
			version:               "1.2.3",
			versionPath:           []string{"status", "resource", "version"},
			expectedName:          "test-resource",
			expectedNs:            "default",
			expectedPath:          []string{"spec", "values", "image", "tag"},
			expectedVersion:       "1.2.3",
		},
		{
			name:                  "oci image without annotations",
			repo:                  "oci",
			artifact:              "image",
			forAnnotation:         "",
			pathAnnotation:        "",
			versionPathAnnotation: "",
			resourceName:          "test-resource",
			resourceNs:            "default",
			version:               "2.0.0",
			versionPath:           []string{"status", "resource", "version"},
			expectedName:          "test-resource",
			expectedNs:            "default",
			expectedPath:          []string{"spec", "values", "image", "tag"},
			expectedVersion:       "2.0.0",
		},
		{
			name:                  "helm image with for annotation - name only",
			repo:                  "helm",
			artifact:              "image",
			forAnnotation:         "target-release",
			pathAnnotation:        "",
			versionPathAnnotation: "",
			resourceName:          "test-resource",
			resourceNs:            "default",
			version:               "1.2.3",
			versionPath:           []string{"status", "resource", "version"},
			expectedName:          "target-release",
			expectedNs:            "default",
			expectedPath:          []string{"spec", "values", "image", "tag"},
			expectedVersion:       "1.2.3",
		},
		{
			name:                  "helm image with for annotation - namespace and name",
			repo:                  "helm",
			artifact:              "image",
			forAnnotation:         "target-namespace/target-release",
			pathAnnotation:        "",
			versionPathAnnotation: "",
			resourceName:          "test-resource",
			resourceNs:            "default",
			version:               "1.2.3",
			versionPath:           []string{"status", "resource", "version"},
			expectedName:          "target-release",
			expectedNs:            "target-namespace",
			expectedPath:          []string{"spec", "values", "image", "tag"},
			expectedVersion:       "1.2.3",
		},
		{
			name:                  "oci image with custom path annotation",
			repo:                  "oci",
			artifact:              "image",
			forAnnotation:         "",
			pathAnnotation:        "container.imageTag",
			versionPathAnnotation: "",
			resourceName:          "test-resource",
			resourceNs:            "default",
			version:               "2.0.0",
			versionPath:           []string{"status", "resource", "version"},
			expectedName:          "test-resource",
			expectedNs:            "default",
			expectedPath:          []string{"spec", "values", "container", "imageTag"},
			expectedVersion:       "2.0.0",
		},
		{
			name:                  "helm image with both for and path annotations",
			repo:                  "helm",
			artifact:              "image",
			forAnnotation:         "other-namespace/other-release",
			pathAnnotation:        "app.version",
			versionPathAnnotation: "",
			resourceName:          "test-resource",
			resourceNs:            "default",
			version:               "3.0.0",
			versionPath:           []string{"status", "resource", "version"},
			expectedName:          "other-release",
			expectedNs:            "other-namespace",
			expectedPath:          []string{"spec", "values", "app", "version"},
			expectedVersion:       "3.0.0",
		},
		{
			name:                  "helm image with custom version-path annotation",
			repo:                  "helm",
			artifact:              "image",
			forAnnotation:         "",
			pathAnnotation:        "",
			versionPathAnnotation: "spec.imageVersion",
			resourceName:          "test-resource",
			resourceNs:            "default",
			version:               "4.5.6",
			versionPath:           []string{"spec", "imageVersion"},
			expectedName:          "test-resource",
			expectedNs:            "default",
			expectedPath:          []string{"spec", "values", "image", "tag"},
			expectedVersion:       "4.5.6",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			ctx := s.T().Context()

			annotations := map[string]any{
				"artifact": tt.artifact,
				"repo":     tt.repo,
			}
			if tt.forAnnotation != "" {
				annotations["for"] = tt.forAnnotation
			}
			if tt.pathAnnotation != "" {
				annotations["path"] = tt.pathAnnotation
			}
			if tt.versionPathAnnotation != "" {
				annotations["version-path"] = tt.versionPathAnnotation
			}

			inst := &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "delivery.ocm.software/v1alpha1",
					"kind":       "Resource",
					"metadata": map[string]any{
						"name":        tt.resourceName,
						"namespace":   tt.resourceNs,
						"annotations": annotations,
					},
					"status": map[string]any{
						"resource": map[string]any{
							"version": tt.version,
						},
					},
					"spec": map[string]any{},
				},
			}

			if err := unstructured.SetNestedField(inst.Object, tt.version, tt.versionPath...); err != nil {
				s.Require().NoError(err)
			}

			clientMock := new(mocks.Client)
			subroutine := NewResourceSubroutine(clientMock, nil, nil)

			clientMock.On("List", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
			clientMock.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(
				func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					if _, ok := obj.(*corev1.ConfigMap); ok {
						return apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, "")
					}
					unstr := obj.(*unstructured.Unstructured)
					unstr.SetName(key.Name)
					unstr.SetNamespace(key.Namespace)
					unstr.Object["spec"] = map[string]any{"values": map[string]any{}}
					return nil
				},
			)
			clientMock.EXPECT().Update(mock.Anything, mock.MatchedBy(func(obj ctrlruntimeclient.Object) bool {
				helmRelease, ok := obj.(*unstructured.Unstructured)
				if !ok {
					return false
				}
				if helmRelease.GetName() != tt.expectedName {
					return false
				}
				if helmRelease.GetNamespace() != tt.expectedNs {
					return false
				}
				actualVersion, found, err := unstructured.NestedString(helmRelease.Object, tt.expectedPath...)
				if err != nil || !found {
					return false
				}
				return actualVersion == tt.expectedVersion
			}), mock.Anything).Return(nil)

			result, err := subroutine.Process(ctx, inst)
			s.Nil(err)
			s.NotNil(result)
		})
	}
}

func (s *ResourceTestSuite) Test_updateGitRepo() {
	ctx := s.T().Context()

	inst := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "delivery.ocm.software/v1alpha1",
			"kind":       "Resource",
			"metadata": map[string]any{
				"name":      "test-git-resource",
				"namespace": "default",
				"annotations": map[string]any{
					"artifact": "chart",
					"repo":     "git",
				},
			},
			"status": map[string]any{
				"resource": map[string]any{
					"access": map[string]any{
						"type":    "gitHub",
						"commit":  "abc123def456",
						"repoUrl": "https://github.com/example/repo.git",
					},
				},
			},
			"spec": map[string]any{},
		},
	}

	clientMock := new(mocks.Client)
	s.subroutine = NewResourceSubroutine(clientMock, nil, nil)

	clientMock.On("List", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	clientMock.On("Get", mock.Anything, mock.Anything, mock.AnythingOfType("*v1.ConfigMap"), mock.Anything).Return(apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, "")).Maybe()
	clientMock.EXPECT().Patch(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, obj ctrlruntimeclient.Object, patch ctrlruntimeclient.Patch, opts ...ctrlruntimeclient.PatchOption) error {
			gitRepo := obj.(*unstructured.Unstructured)

			commit, found, err := unstructured.NestedString(gitRepo.Object, "spec", "ref", "commit")
			s.Require().NoError(err)
			s.Require().True(found)
			s.Require().Equal("abc123def456", commit)

			url, found, err := unstructured.NestedString(gitRepo.Object, "spec", "url")
			s.Require().NoError(err)
			s.Require().True(found)
			s.Require().Equal("https://github.com/example/repo.git", url)

			interval, found, err := unstructured.NestedString(gitRepo.Object, "spec", "interval")
			s.Require().NoError(err)
			s.Require().True(found)
			s.Require().Equal("1m0s", interval)

			return nil
		},
	)

	result, err := s.subroutine.Process(ctx, inst)
	s.Nil(err)
	s.NotNil(result)
}

func (s *ResourceTestSuite) Test_updateGitRepo_CreateOrUpdateError() {
	ctx := s.T().Context()

	inst := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "delivery.ocm.software/v1alpha1",
			"kind":       "Resource",
			"metadata": map[string]any{
				"name":      "test-git-resource",
				"namespace": "default",
				"annotations": map[string]any{
					"artifact": "chart",
					"repo":     "git",
				},
			},
			"status": map[string]any{
				"resource": map[string]any{
					"access": map[string]any{
						"type":    "gitHub",
						"commit":  "abc123def456",
						"repoUrl": "https://github.com/example/repo.git",
					},
				},
			},
			"spec": map[string]any{},
		},
	}

	clientMock := new(mocks.Client)
	s.subroutine = NewResourceSubroutine(clientMock, nil, nil)

	clientMock.On("List", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	clientMock.On("Get", mock.Anything, mock.Anything, mock.AnythingOfType("*v1.ConfigMap"), mock.Anything).Return(apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, "")).Maybe()
	clientMock.EXPECT().Patch(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(errors.New("client error"))

	result, err := s.subroutine.Process(ctx, inst)
	s.NotNil(err)
	s.NotNil(result)
}

func (s *ResourceTestSuite) Test_updateHelmRepository() {
	ctx := s.T().Context()

	inst := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "delivery.ocm.software/v1alpha1",
			"kind":       "Resource",
			"metadata": map[string]any{
				"name":      "test-helm-resource",
				"namespace": "default",
				"annotations": map[string]any{
					"artifact": "chart",
					"repo":     "helm",
				},
			},
			"status": map[string]any{
				"resource": map[string]any{
					"version": "1.2.3",
					"access": map[string]any{
						"type":           "helmChart",
						"helmRepository": "https://charts.example.com",
					},
				},
			},
			"spec": map[string]any{},
		},
	}

	clientMock := new(mocks.Client)
	s.subroutine = NewResourceSubroutine(clientMock, nil, nil)

	clientMock.On("List", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	clientMock.EXPECT().Patch(mock.Anything, mock.MatchedBy(func(obj ctrlruntimeclient.Object) bool {
		unstr := obj.(*unstructured.Unstructured)
		if unstr.GetKind() != "HelmRepository" {
			return false
		}
		url, found, err := unstructured.NestedString(unstr.Object, "spec", "url")
		if err != nil || !found || url != "https://charts.example.com" {
			return false
		}
		provider, found, err := unstructured.NestedString(unstr.Object, "spec", "provider")
		if err != nil || !found || provider != "generic" {
			return false
		}
		interval, found, err := unstructured.NestedString(unstr.Object, "spec", "interval")
		if err != nil || !found || interval != "5m" {
			return false
		}
		return true
	}), mock.Anything, mock.Anything, mock.Anything).Return(nil).Times(1)
	clientMock.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			if _, ok := obj.(*corev1.ConfigMap); ok {
				return apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, "")
			}
			unstr := obj.(*unstructured.Unstructured)
			unstr.SetName(key.Name)
			unstr.SetNamespace(key.Namespace)
			unstr.Object["spec"] = map[string]any{"chart": map[string]any{"spec": map[string]any{}}}
			return nil
		},
	)
	clientMock.EXPECT().Update(mock.Anything, mock.MatchedBy(func(obj ctrlruntimeclient.Object) bool {
		unstr := obj.(*unstructured.Unstructured)
		version, found, err := unstructured.NestedString(unstr.Object, "spec", "chart", "spec", "version")
		return err == nil && found && version == "1.2.3"
	}), mock.Anything).Return(nil).Times(1)

	result, err := s.subroutine.Process(ctx, inst)
	s.Nil(err)
	s.NotNil(result)
}

func (s *ResourceTestSuite) Test_updateHelmRepository_MissingURL() {
	ctx := s.T().Context()

	inst := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "delivery.ocm.software/v1alpha1",
			"kind":       "Resource",
			"metadata": map[string]any{
				"name":      "test-helm-resource",
				"namespace": "default",
				"annotations": map[string]any{
					"artifact": "chart",
					"repo":     "helm",
				},
			},
			"status": map[string]any{
				"resource": map[string]any{
					"version": "1.2.3",
					"access":  map[string]any{},
				},
			},
			"spec": map[string]any{},
		},
	}

	clientMock := new(mocks.Client)
	s.subroutine = NewResourceSubroutine(clientMock, nil, nil)

	clientMock.On("List", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	result, err := s.subroutine.Process(ctx, inst)
	s.Nil(err)
	s.True(result.IsStopWithRequeue())
}

func (s *ResourceTestSuite) Test_updateHelmRelease() {
	ctx := s.T().Context()

	inst := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "delivery.ocm.software/v1alpha1",
			"kind":       "Resource",
			"metadata": map[string]any{
				"name":      "test-helm-resource",
				"namespace": "default",
				"annotations": map[string]any{
					"artifact": "chart",
					"repo":     "helm",
				},
			},
			"status": map[string]any{
				"resource": map[string]any{
					"version": "2.5.0",
					"access": map[string]any{
						"type":           "helmChart",
						"helmRepository": "https://charts.example.com",
					},
				},
			},
			"spec": map[string]any{},
		},
	}

	clientMock := new(mocks.Client)
	subroutine := NewResourceSubroutine(clientMock, nil, nil)

	clientMock.On("List", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	clientMock.EXPECT().Patch(mock.Anything, mock.MatchedBy(func(obj ctrlruntimeclient.Object) bool {
		unstr := obj.(*unstructured.Unstructured)
		return unstr.GetKind() == "HelmRepository"
	}), mock.Anything, mock.Anything, mock.Anything).Return(nil).Times(1)
	clientMock.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			if _, ok := obj.(*corev1.ConfigMap); ok {
				return apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, "")
			}
			unstr := obj.(*unstructured.Unstructured)
			unstr.SetName(key.Name)
			unstr.SetNamespace(key.Namespace)
			unstr.Object["spec"] = map[string]any{"chart": map[string]any{"spec": map[string]any{}}}
			return nil
		},
	)
	clientMock.EXPECT().Update(mock.Anything, mock.MatchedBy(func(obj ctrlruntimeclient.Object) bool {
		unstr := obj.(*unstructured.Unstructured)
		version, found, err := unstructured.NestedString(unstr.Object, "spec", "chart", "spec", "version")
		return err == nil && found && version == "2.5.0"
	}), mock.Anything).Return(nil).Times(1)

	result, err := subroutine.Process(ctx, inst)
	s.Nil(err)
	s.NotNil(result)
}

func (s *ResourceTestSuite) Test_updateHelmRelease_GetError() {
	ctx := s.T().Context()

	inst := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "delivery.ocm.software/v1alpha1",
			"kind":       "Resource",
			"metadata": map[string]any{
				"name":      "test-helm-resource",
				"namespace": "default",
				"annotations": map[string]any{
					"artifact": "chart",
					"repo":     "helm",
				},
			},
			"status": map[string]any{
				"resource": map[string]any{
					"version": "2.5.0",
					"access": map[string]any{
						"type":           "helmChart",
						"helmRepository": "https://charts.example.com",
					},
				},
			},
			"spec": map[string]any{},
		},
	}

	clientMock := new(mocks.Client)
	subroutine := NewResourceSubroutine(clientMock, nil, nil)

	clientMock.On("List", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	clientMock.EXPECT().Patch(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Times(1)
	clientMock.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(errors.New("get error"))

	result, err := subroutine.Process(ctx, inst)
	s.NotNil(err)
	s.NotNil(result)
}

func (s *ResourceTestSuite) Test_updateHelmRelease_UpdateError() {
	ctx := s.T().Context()

	inst := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "delivery.ocm.software/v1alpha1",
			"kind":       "Resource",
			"metadata": map[string]any{
				"name":      "test-helm-resource",
				"namespace": "default",
				"annotations": map[string]any{
					"artifact": "chart",
					"repo":     "helm",
				},
			},
			"status": map[string]any{
				"resource": map[string]any{
					"version": "2.5.0",
					"access": map[string]any{
						"type":           "helmChart",
						"helmRepository": "https://charts.example.com",
					},
				},
			},
			"spec": map[string]any{},
		},
	}

	clientMock := new(mocks.Client)
	subroutine := NewResourceSubroutine(clientMock, nil, nil)

	clientMock.On("List", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	clientMock.EXPECT().Patch(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Times(1)
	clientMock.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			if _, ok := obj.(*corev1.ConfigMap); ok {
				return apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, "")
			}
			unstr := obj.(*unstructured.Unstructured)
			unstr.SetName(key.Name)
			unstr.SetNamespace(key.Namespace)
			unstr.Object["spec"] = map[string]any{"chart": map[string]any{"spec": map[string]any{}}}
			return nil
		},
	)
	clientMock.EXPECT().Update(mock.Anything, mock.Anything, mock.Anything).Return(errors.New("update error")).Times(1)

	result, err := subroutine.Process(ctx, inst)
	s.NotNil(err)
	s.NotNil(result)
}

func (s *ResourceTestSuite) Test_updateHelmReleaseWithImageTag_GetError() {
	ctx := s.T().Context()

	inst := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "delivery.ocm.software/v1alpha1",
			"kind":       "Resource",
			"metadata": map[string]any{
				"name":      "test-resource",
				"namespace": "default",
				"annotations": map[string]any{
					"artifact": "image",
					"repo":     "helm",
				},
			},
			"status": map[string]any{
				"resource": map[string]any{
					"version": "1.2.3",
				},
			},
			"spec": map[string]any{},
		},
	}

	clientMock := new(mocks.Client)
	subroutine := NewResourceSubroutine(clientMock, nil, nil)

	clientMock.On("List", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	clientMock.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(errors.New("get error"))

	result, err := subroutine.Process(ctx, inst)
	s.NotNil(err)
	s.NotNil(result)
}

func (s *ResourceTestSuite) Test_updateHelmReleaseWithImageTag_UpdateError() {
	ctx := s.T().Context()

	inst := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "delivery.ocm.software/v1alpha1",
			"kind":       "Resource",
			"metadata": map[string]any{
				"name":      "test-resource",
				"namespace": "default",
				"annotations": map[string]any{
					"artifact": "image",
					"repo":     "helm",
				},
			},
			"status": map[string]any{
				"resource": map[string]any{
					"version": "1.2.3",
				},
			},
			"spec": map[string]any{},
		},
	}

	clientMock := new(mocks.Client)
	subroutine := NewResourceSubroutine(clientMock, nil, nil)

	clientMock.On("List", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	clientMock.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			if _, ok := obj.(*corev1.ConfigMap); ok {
				return apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, "")
			}
			unstr := obj.(*unstructured.Unstructured)
			unstr.SetName(key.Name)
			unstr.SetNamespace(key.Namespace)
			unstr.Object["spec"] = map[string]any{"values": map[string]any{}}
			return nil
		},
	)
	clientMock.EXPECT().Update(mock.Anything, mock.Anything, mock.Anything).Return(errors.New("update error"))

	result, err := subroutine.Process(ctx, inst)
	s.NotNil(err)
	s.NotNil(result)
}

func (s *ResourceTestSuite) Test_updateOciRepo_ParseRefError() {
	ctx := s.T().Context()

	inst := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "delivery.ocm.software/v1alpha1",
			"kind":       "Resource",
			"metadata": map[string]any{
				"name":      "test-resource",
				"namespace": "default",
				"annotations": map[string]any{
					"artifact": "chart",
					"repo":     "oci",
				},
			},
			"status": map[string]any{
				"resource": map[string]any{
					"version": "1.0.0",
					"access": map[string]any{
						"type":           "ociArtifact",
						"imageReference": "oci://invalid url with spaces",
					},
				},
			},
			"spec": map[string]any{},
		},
	}

	clientMock := new(mocks.Client)
	s.subroutine = NewResourceSubroutine(clientMock, nil, nil)

	clientMock.On("List", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	result, err := s.subroutine.Process(ctx, inst)
	s.NotNil(err)
	s.NotNil(result)
}

func (s *ResourceTestSuite) Test_updateOciRepo_PlainHTTPRegistryIsInsecure() {
	ctx := s.T().Context()

	newResource := func(imageReference string) *unstructured.Unstructured {
		return &unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "delivery.ocm.software/v1alpha1",
				"kind":       "Resource",
				"metadata": map[string]any{
					"name":      "test-resource",
					"namespace": "default",
					"annotations": map[string]any{
						"artifact": "chart",
						"repo":     "oci",
					},
				},
				"status": map[string]any{
					"resource": map[string]any{
						"version": "1.0.0",
						"access": map[string]any{
							"type":           "ociArtifact",
							"imageReference": imageReference,
						},
					},
				},
				"spec": map[string]any{},
			},
		}
	}

	tests := []struct {
		name           string
		imageReference string
		wantURL        string
		wantInsecure   bool
	}{
		{
			name:           "plain-HTTP registry sets spec.insecure",
			imageReference: "http://172.18.0.5:5000/platform-mesh/helm-charts/mychart:1.0.0",
			wantURL:        "oci://172.18.0.5:5000/platform-mesh/helm-charts/mychart",
			wantInsecure:   true,
		},
		{
			name:           "TLS registry leaves spec.insecure unset",
			imageReference: "ghcr.io/platform-mesh/helm-charts/mychart:1.0.0",
			wantURL:        "oci://ghcr.io/platform-mesh/helm-charts/mychart",
			wantInsecure:   false,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			clientMock := new(mocks.Client)
			s.subroutine = NewResourceSubroutine(clientMock, nil, nil)

			clientMock.On("List", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
			clientMock.On("Get", mock.Anything, mock.Anything, mock.AnythingOfType("*v1.ConfigMap"), mock.Anything).Return(apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, "")).Maybe()
			clientMock.EXPECT().Patch(mock.Anything, mock.MatchedBy(func(obj ctrlruntimeclient.Object) bool {
				unstr := obj.(*unstructured.Unstructured)
				if unstr.GetKind() != "OCIRepository" {
					return false
				}
				url, _, _ := unstructured.NestedString(unstr.Object, "spec", "url")
				if url != tt.wantURL {
					return false
				}
				insecure, found, _ := unstructured.NestedBool(unstr.Object, "spec", "insecure")
				if tt.wantInsecure {
					return found && insecure
				}
				return !found
			}), mock.Anything, mock.Anything, mock.Anything).Return(nil).Times(1)

			result, err := s.subroutine.Process(ctx, newResource(tt.imageReference))
			s.NoError(err)
			s.NotNil(result)
		})
	}
}

func (s *ResourceTestSuite) Test_updateOciRepo_CreateOrUpdateError() {
	ctx := s.T().Context()

	inst := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "delivery.ocm.software/v1alpha1",
			"kind":       "Resource",
			"metadata": map[string]any{
				"name":      "test-resource",
				"namespace": "default",
				"annotations": map[string]any{
					"artifact": "chart",
					"repo":     "oci",
				},
			},
			"status": map[string]any{
				"resource": map[string]any{
					"version": "1.0.0",
					"access": map[string]any{
						"type":           "ociArtifact",
						"imageReference": "oci://registry.example.com/charts/mychart:1.0.0",
					},
				},
			},
			"spec": map[string]any{},
		},
	}

	clientMock := new(mocks.Client)
	s.subroutine = NewResourceSubroutine(clientMock, nil, nil)

	clientMock.On("List", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	clientMock.On("Get", mock.Anything, mock.Anything, mock.AnythingOfType("*v1.ConfigMap"), mock.Anything).Return(apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, "")).Maybe()
	clientMock.EXPECT().Patch(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(errors.New("client error"))

	result, err := s.subroutine.Process(ctx, inst)
	s.NotNil(err)
	s.NotNil(result)
}

func (s *ResourceTestSuite) Test_Process_NoAnnotations() {
	ctx := s.T().Context()

	inst := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "delivery.ocm.software/v1alpha1",
			"kind":       "Resource",
			"metadata": map[string]any{
				"name":      "test-resource",
				"namespace": "default",
			},
			"spec": map[string]any{},
		},
	}

	result, err := s.subroutine.Process(ctx, inst)
	s.Nil(err)
	s.NotNil(result)
}

func (s *ResourceTestSuite) Test_updateArgoCDApplication_HelmRepo() {
	ctx := s.T().Context()

	inst := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "delivery.ocm.software/v1alpha1",
			"kind":       "Resource",
			"metadata": map[string]any{
				"name":      "keycloak-chart",
				"namespace": "platform-mesh-system",
				"annotations": map[string]any{
					"artifact": "chart",
					"repo":     "helm",
				},
			},
			"status": map[string]any{
				"resource": map[string]any{
					"version": "25.2.3",
					"access": map[string]any{
						"type":           "helmChart",
						"helmRepository": "https://charts.bitnami.com/bitnami",
					},
				},
			},
			"spec": map[string]any{},
		},
	}

	clientMock := new(mocks.Client)
	sub := NewResourceSubroutine(clientMock, nil, nil)

	clientMock.On("List", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("no CRD")).Maybe()
	clientMock.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			if key.Name == "platform-mesh-profile" || key.Name == "platform-mesh-system-profile" {
				cm := obj.(*corev1.ConfigMap)
				cm.Data = map[string]string{"profile.yaml": "infra:\n  deploymentTechnology: argocd\n"}
				return nil
			}
			unstr := obj.(*unstructured.Unstructured)
			unstr.SetName(key.Name)
			unstr.SetNamespace(key.Namespace)
			_ = unstructured.SetNestedField(unstr.Object, "https://old-repo.com", "spec", "source", "repoURL")
			_ = unstructured.SetNestedField(unstr.Object, "1.0.0", "spec", "source", "targetRevision")
			return nil
		},
	)
	clientMock.EXPECT().Patch(mock.Anything, mock.MatchedBy(func(obj ctrlruntimeclient.Object) bool {
		unstr := obj.(*unstructured.Unstructured)
		repoURL, _, _ := unstructured.NestedString(unstr.Object, "spec", "source", "repoURL")
		rev, _, _ := unstructured.NestedString(unstr.Object, "spec", "source", "targetRevision")
		return repoURL == "https://charts.bitnami.com/bitnami" && rev == "25.2.3" && unstr.GetName() == "keycloak"
	}), mock.Anything, mock.Anything, mock.Anything).Return(nil)

	result, err := sub.Process(ctx, inst)
	s.Nil(err)
	s.NotNil(result)
}

func (s *ResourceTestSuite) Test_updateArgoCDApplication_AlreadyUpToDate() {
	ctx := s.T().Context()

	inst := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "delivery.ocm.software/v1alpha1",
			"kind":       "Resource",
			"metadata": map[string]any{
				"name":        "keycloak-chart",
				"namespace":   "platform-mesh-system",
				"annotations": map[string]any{"artifact": "chart", "repo": "helm"},
			},
			"status": map[string]any{
				"resource": map[string]any{
					"version": "25.2.3",
					"access":  map[string]any{"type": "helmChart", "helmRepository": "https://charts.bitnami.com/bitnami"},
				},
			},
			"spec": map[string]any{},
		},
	}

	clientMock := new(mocks.Client)
	sub := NewResourceSubroutine(clientMock, nil, nil)

	clientMock.On("List", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("no CRD")).Maybe()
	clientMock.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			if key.Name == "platform-mesh-profile" || key.Name == "platform-mesh-system-profile" {
				cm := obj.(*corev1.ConfigMap)
				cm.Data = map[string]string{"profile.yaml": "infra:\n  deploymentTechnology: argocd\n"}
				return nil
			}
			unstr := obj.(*unstructured.Unstructured)
			unstr.SetName(key.Name)
			unstr.SetNamespace(key.Namespace)
			_ = unstructured.SetNestedField(unstr.Object, "https://charts.bitnami.com/bitnami", "spec", "source", "repoURL")
			_ = unstructured.SetNestedField(unstr.Object, "25.2.3", "spec", "source", "targetRevision")
			return nil
		},
	)

	result, err := sub.Process(ctx, inst)
	s.Nil(err)
	s.NotNil(result)
}

func (s *ResourceTestSuite) Test_updateArgoCDApplicationHelmValues() {
	ctx := s.T().Context()

	inst := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "delivery.ocm.software/v1alpha1",
			"kind":       "Resource",
			"metadata": map[string]any{
				"name":        "kcp-image",
				"namespace":   "platform-mesh-system",
				"annotations": map[string]any{"artifact": "image", "repo": "oci", "path": "kcp.image.tag"},
			},
			"status": map[string]any{
				"resource": map[string]any{"version": "v0.30.0"},
			},
			"spec": map[string]any{},
		},
	}

	clientMock := new(mocks.Client)
	store := subroutines.NewImageVersionStore()
	sub := NewResourceSubroutine(clientMock, nil, store)

	clientMock.On("List", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("no CRD")).Maybe()
	clientMock.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			if key.Name == "platform-mesh-profile" || key.Name == "platform-mesh-system-profile" {
				cm := obj.(*corev1.ConfigMap)
				cm.Data = map[string]string{"profile.yaml": "infra:\n  deploymentTechnology: argocd\n"}
				return nil
			}
			unstr := obj.(*unstructured.Unstructured)
			unstr.SetName(key.Name)
			unstr.SetNamespace(key.Namespace)
			_ = unstructured.SetNestedField(unstr.Object, "kcp:\n  image:\n    tag: v0.29.0\n", "spec", "source", "helm", "values")
			return nil
		},
	)
	clientMock.EXPECT().Patch(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	result, err := sub.Process(ctx, inst)
	s.Nil(err)
	s.NotNil(result)

	versions := store.Get("platform-mesh-system", "kcp")
	s.Require().Len(versions, 1)
	s.Equal("kcp.image.tag", versions[0].Path)
	s.Equal("v0.30.0", versions[0].Version)
}

func (s *ResourceTestSuite) Test_resolveArgoCDSource_OCI() {
	inst := &unstructured.Unstructured{
		Object: map[string]any{
			"status": map[string]any{
				"resource": map[string]any{
					"version": "1.2.3",
					"access":  map[string]any{"imageReference": "oci://registry.example.com/charts/mychart:1.2.3@sha256:abc"},
				},
			},
		},
	}
	repoURL, rev, chartType, err := s.subroutine.resolveArgoCDSource(inst)
	s.Nil(err)
	s.Equal("registry.example.com/charts", repoURL)
	s.Equal("1.2.3", rev)
	s.Equal("oci", chartType)
}

func (s *ResourceTestSuite) Test_resolveArgoCDSource_NoSource() {
	inst := &unstructured.Unstructured{
		Object: map[string]any{"status": map[string]any{"resource": map[string]any{"access": map[string]any{}}}},
	}
	_, _, _, err := s.subroutine.resolveArgoCDSource(inst)
	s.NotNil(err)
	s.Contains(err.Error(), "no helmRepository, repoUrl, or imageReference found")
}

func (s *ResourceTestSuite) Test_resolveArgoCDSource_HelmNoVersion() {
	inst := &unstructured.Unstructured{
		Object: map[string]any{"status": map[string]any{"resource": map[string]any{"access": map[string]any{"helmRepository": "https://charts.example.com"}}}},
	}
	_, _, _, err := s.subroutine.resolveArgoCDSource(inst)
	s.NotNil(err)
	s.Contains(err.Error(), "version not found for helm chart")
}

func (s *ResourceTestSuite) Test_resolveArgoCDSource_GitNoRef() {
	inst := &unstructured.Unstructured{
		Object: map[string]any{"status": map[string]any{"resource": map[string]any{"access": map[string]any{"repoUrl": "https://github.com/org/repo"}}}},
	}
	_, _, _, err := s.subroutine.resolveArgoCDSource(inst)
	s.NotNil(err)
	s.Contains(err.Error(), "no ref, version, or commit found")
}

func Test_firstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "", "c"); got != "c" {
		t.Errorf("got %q want %q", got, "c")
	}
	if got := firstNonEmpty("a", "b"); got != "a" {
		t.Errorf("got %q want %q", got, "a")
	}
	if got := firstNonEmpty("", ""); got != "" {
		t.Errorf("got %q want %q", got, "")
	}
}

func Test_getValueFromYAML(t *testing.T) {
	yamlStr := "kcp:\n  image:\n    tag: v0.30.0\n"
	if got := getValueFromYAML(yamlStr, []string{"kcp", "image", "tag"}); got != "v0.30.0" {
		t.Errorf("got %q want %q", got, "v0.30.0")
	}
	if got := getValueFromYAML(yamlStr, []string{"missing"}); got != "" {
		t.Errorf("got %q want empty", got)
	}
	if got := getValueFromYAML("", []string{"a"}); got != "" {
		t.Errorf("got %q want empty", got)
	}
}

func Test_getNestedString(t *testing.T) {
	m := map[string]any{"a": map[string]any{"b": "hello"}}
	got, ok := getNestedString(m, "a", "b")
	if !ok || got != "hello" {
		t.Errorf("got %q, ok=%v", got, ok)
	}
	_, ok = getNestedString(m, "x", "y")
	if ok {
		t.Error("expected ok=false")
	}
	_, ok = getNestedString(m)
	if ok {
		t.Error("expected ok=false for empty path")
	}
	m2 := map[string]any{"a": 42}
	_, ok = getNestedString(m2, "a")
	if ok {
		t.Error("expected ok=false for non-string")
	}
}

func (s *ResourceTestSuite) Test_SetRuntimeClient() {
	clientMock := new(mocks.Client)
	sub := NewResourceSubroutine(s.clientMock, nil, nil)
	sub.SetRuntimeClient(clientMock)
	s.Equal(clientMock, sub.clientRuntime)
}

func (s *ResourceTestSuite) Test_getAppNamespaceFromProfile_InfraDeploymentNamespace() {
	ctx := s.T().Context()
	clientMock := new(mocks.Client)
	sub := NewResourceSubroutine(clientMock, nil, nil)

	clientMock.EXPECT().Get(mock.Anything, mock.MatchedBy(func(key ctrlruntimeclient.ObjectKey) bool {
		return key.Name == "platform-mesh-profile" && key.Namespace == "platform-mesh-system"
	}), mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			cm := obj.(*corev1.ConfigMap)
			cm.Data = map[string]string{"profile.yaml": "infra:\n  deploymentNamespace: my-apps\n  deploymentTechnology: argocd\n"}
			return nil
		},
	)

	log := logger.LoadLoggerFromContext(ctx)
	ns, err := sub.getAppNamespaceFromProfile(ctx, "platform-mesh-system", log)
	s.Require().NoError(err)
	s.Equal("my-apps", ns)
}

func (s *ResourceTestSuite) Test_getAppNamespaceFromProfile_ComponentsDeploymentNamespace() {
	ctx := s.T().Context()
	clientMock := new(mocks.Client)
	sub := NewResourceSubroutine(clientMock, nil, nil)

	clientMock.EXPECT().Get(mock.Anything, mock.MatchedBy(func(key ctrlruntimeclient.ObjectKey) bool {
		return key.Name == "platform-mesh-profile" && key.Namespace == "platform-mesh-system"
	}), mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			cm := obj.(*corev1.ConfigMap)
			cm.Data = map[string]string{"profile.yaml": "components:\n  deploymentNamespace: my-apps-int\n"}
			return nil
		},
	)

	log := logger.LoadLoggerFromContext(ctx)
	ns, err := sub.getAppNamespaceFromProfile(ctx, "platform-mesh-system", log)
	s.Require().NoError(err)
	s.Equal("my-apps-int", ns)
}

func (s *ResourceTestSuite) Test_getAppNamespaceFromProfile_Fallback() {
	ctx := s.T().Context()
	clientMock := new(mocks.Client)
	sub := NewResourceSubroutine(clientMock, nil, nil)

	clientMock.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, "platform-mesh-profile"))

	log := logger.LoadLoggerFromContext(ctx)
	ns, err := sub.getAppNamespaceFromProfile(ctx, "platform-mesh-system", log)
	s.Require().NoError(err)
	s.Equal("platform-mesh-system", ns)
}

func (s *ResourceTestSuite) Test_getAppNamespaceFromProfile_NoDeploymentNamespace() {
	ctx := s.T().Context()
	clientMock := new(mocks.Client)
	sub := NewResourceSubroutine(clientMock, nil, nil)

	clientMock.EXPECT().Get(mock.Anything, mock.MatchedBy(func(key ctrlruntimeclient.ObjectKey) bool {
		return key.Name == "platform-mesh-profile" && key.Namespace == "platform-mesh-system"
	}), mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			cm := obj.(*corev1.ConfigMap)
			cm.Data = map[string]string{"profile.yaml": "infra:\n  deploymentTechnology: argocd\n"}
			return nil
		},
	)
	clientMock.EXPECT().Get(mock.Anything, mock.MatchedBy(func(key ctrlruntimeclient.ObjectKey) bool {
		return key.Name == "platform-mesh-system-profile" && key.Namespace == "platform-mesh-system"
	}), mock.Anything, mock.Anything).Return(apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, "platform-mesh-system-profile"))

	log := logger.LoadLoggerFromContext(ctx)
	ns, err := sub.getAppNamespaceFromProfile(ctx, "platform-mesh-system", log)
	s.Require().NoError(err)
	s.Equal("platform-mesh-system", ns)
}

func (s *ResourceTestSuite) Test_updateArgoCDApplication_UsesDeploymentNamespace() {
	ctx := s.T().Context()

	inst := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "delivery.ocm.software/v1alpha1",
			"kind":       "Resource",
			"metadata": map[string]any{
				"name":      "keycloak-chart",
				"namespace": "platform-mesh-system",
				"annotations": map[string]any{
					"artifact": "chart",
					"repo":     "helm",
				},
			},
			"status": map[string]any{
				"resource": map[string]any{
					"version": "25.2.3",
					"access": map[string]any{
						"type":           "helmChart",
						"helmRepository": "https://charts.bitnami.com/bitnami",
					},
				},
			},
			"spec": map[string]any{},
		},
	}

	clientMock := new(mocks.Client)
	sub := NewResourceSubroutine(clientMock, nil, nil)

	clientMock.On("List", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("no CRD")).Maybe()
	clientMock.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			if key.Name == "platform-mesh-profile" || key.Name == "platform-mesh-system-profile" {
				cm := obj.(*corev1.ConfigMap)
				cm.Data = map[string]string{"profile.yaml": "infra:\n  deploymentNamespace: my-apps\n  deploymentTechnology: argocd\n"}
				return nil
			}
			unstr := obj.(*unstructured.Unstructured)
			unstr.SetName(key.Name)
			unstr.SetNamespace(key.Namespace)
			_ = unstructured.SetNestedField(unstr.Object, "https://old-repo.com", "spec", "source", "repoURL")
			_ = unstructured.SetNestedField(unstr.Object, "1.0.0", "spec", "source", "targetRevision")
			return nil
		},
	)
	clientMock.EXPECT().Patch(mock.Anything, mock.MatchedBy(func(obj ctrlruntimeclient.Object) bool {
		unstr := obj.(*unstructured.Unstructured)
		return unstr.GetNamespace() == "my-apps" && unstr.GetName() == "keycloak"
	}), mock.Anything, mock.Anything, mock.Anything).Return(nil)

	result, err := sub.Process(ctx, inst)
	s.Nil(err)
	s.NotNil(result)
}

func (s *ResourceTestSuite) Test_updateHelmReleaseImage() {
	tests := []struct {
		name                  string
		repo                  string
		artifact              string
		forAnnotation         string
		pathAnnotation        string
		versionPathAnnotation string
		resourceName          string
		resourceNs            string
		version               string
		versionPath           []string
		expectedName          string
		expectedNs            string
		expectedPath          []string
		expectedVersion       string
		additionalRegistry    string
		additionalRepository  string
		additionalDigest      string
		additionalTag         string
		existingCoords        map[string]string
		expectedRegistry      string
		expectedRepository    string
		expectedDigest        string
	}{
		{
			name:                  "helm image without annotations",
			repo:                  "helm",
			artifact:              "image",
			forAnnotation:         "",
			pathAnnotation:        "",
			versionPathAnnotation: "",
			resourceName:          "test-resource",
			resourceNs:            "default",
			version:               "1.2.3",
			versionPath:           []string{"status", "resource", "version"},
			expectedName:          "test-resource",
			expectedNs:            "default",
			expectedPath:          []string{"spec", "values", "image", "tag"},
			expectedVersion:       "1.2.3",
		},
		{
			name:                  "oci image without annotations",
			repo:                  "oci",
			artifact:              "image",
			forAnnotation:         "",
			pathAnnotation:        "",
			versionPathAnnotation: "",
			resourceName:          "test-resource",
			resourceNs:            "default",
			version:               "2.0.0",
			versionPath:           []string{"status", "resource", "version"},
			expectedName:          "test-resource",
			expectedNs:            "default",
			expectedPath:          []string{"spec", "values", "image", "tag"},
			expectedVersion:       "2.0.0",
		},
		{
			name:                  "helm image with for annotation - name only",
			repo:                  "helm",
			artifact:              "image",
			forAnnotation:         "target-release",
			pathAnnotation:        "",
			versionPathAnnotation: "",
			resourceName:          "test-resource",
			resourceNs:            "default",
			version:               "1.2.3",
			versionPath:           []string{"status", "resource", "version"},
			expectedName:          "target-release",
			expectedNs:            "default",
			expectedPath:          []string{"spec", "values", "image", "tag"},
			expectedVersion:       "1.2.3",
		},
		{
			name:                  "helm image with for annotation - namespace and name",
			repo:                  "helm",
			artifact:              "image",
			forAnnotation:         "target-namespace/target-release",
			pathAnnotation:        "",
			versionPathAnnotation: "",
			resourceName:          "test-resource",
			resourceNs:            "default",
			version:               "1.2.3",
			versionPath:           []string{"status", "resource", "version"},
			expectedName:          "target-release",
			expectedNs:            "target-namespace",
			expectedPath:          []string{"spec", "values", "image", "tag"},
			expectedVersion:       "1.2.3",
		},
		{
			name:                  "oci image with custom path annotation",
			repo:                  "oci",
			artifact:              "image",
			forAnnotation:         "",
			pathAnnotation:        "container.imageTag",
			versionPathAnnotation: "",
			resourceName:          "test-resource",
			resourceNs:            "default",
			version:               "2.0.0",
			versionPath:           []string{"status", "resource", "version"},
			expectedName:          "test-resource",
			expectedNs:            "default",
			expectedPath:          []string{"spec", "values", "container", "imageTag"},
			expectedVersion:       "2.0.0",
		},
		{
			name:                  "helm image with both for and path annotations",
			repo:                  "helm",
			artifact:              "image",
			forAnnotation:         "other-namespace/other-release",
			pathAnnotation:        "app.version",
			versionPathAnnotation: "",
			resourceName:          "test-resource",
			resourceNs:            "default",
			version:               "3.0.0",
			versionPath:           []string{"status", "resource", "version"},
			expectedName:          "other-release",
			expectedNs:            "other-namespace",
			expectedPath:          []string{"spec", "values", "app", "version"},
			expectedVersion:       "3.0.0",
		},
		{
			name:                  "helm image with custom version-path annotation",
			repo:                  "helm",
			artifact:              "image",
			forAnnotation:         "",
			pathAnnotation:        "",
			versionPathAnnotation: "spec.imageVersion",
			resourceName:          "test-resource",
			resourceNs:            "default",
			version:               "4.5.6",
			versionPath:           []string{"spec", "imageVersion"},
			expectedName:          "test-resource",
			expectedNs:            "default",
			expectedPath:          []string{"spec", "values", "image", "tag"},
			expectedVersion:       "4.5.6",
		},
		{
			name:                 "localized image - injects registry, repository and tag",
			repo:                 "oci",
			artifact:             "image",
			resourceName:         "test-resource",
			resourceNs:           "default",
			version:              "1.2.3",
			versionPath:          []string{"status", "resource", "version"},
			additionalRegistry:   "oci-registry-docker-registry.registry.svc.cluster.local",
			additionalRepository: "platform-mesh/account-operator",
			expectedName:         "test-resource",
			expectedNs:           "default",
			expectedPath:         []string{"spec", "values", "image", "tag"},
			expectedVersion:      "1.2.3",
			expectedRegistry:     "oci-registry-docker-registry.registry.svc.cluster.local",
			expectedRepository:   "platform-mesh/account-operator",
		},
		{
			name:                 "localized image with digest - injects digest as well",
			repo:                 "oci",
			artifact:             "image",
			resourceName:         "test-resource",
			resourceNs:           "default",
			version:              "1.2.3",
			versionPath:          []string{"status", "resource", "version"},
			additionalRegistry:   "oci-registry-docker-registry.registry.svc.cluster.local",
			additionalRepository: "platform-mesh/account-operator",
			additionalDigest:     "sha256:cb5be99827d7cfa107fc7ca06f5b2fb0ea486f3ffb0315baf2be1bb348f9db77",
			expectedName:         "test-resource",
			expectedNs:           "default",
			expectedPath:         []string{"spec", "values", "image", "tag"},
			expectedVersion:      "1.2.3",
			expectedRegistry:     "oci-registry-docker-registry.registry.svc.cluster.local",
			expectedRepository:   "platform-mesh/account-operator",
			expectedDigest:       "sha256:cb5be99827d7cfa107fc7ca06f5b2fb0ea486f3ffb0315baf2be1bb348f9db77",
		},
		{
			name:            "no localized coordinates - falls back to tag-only injection",
			repo:            "oci",
			artifact:        "image",
			resourceName:    "test-resource",
			resourceNs:      "default",
			version:         "1.2.3",
			versionPath:     []string{"status", "resource", "version"},
			expectedName:    "test-resource",
			expectedNs:      "default",
			expectedPath:    []string{"spec", "values", "image", "tag"},
			expectedVersion: "1.2.3",
		},
		{
			// The localized access tag (status.additional.tag) takes precedence over
			// the resource version when writing the tag leaf.
			name:                 "localized image - access tag overrides resource version",
			repo:                 "oci",
			artifact:             "image",
			resourceName:         "test-resource",
			resourceNs:           "default",
			version:              "1.2.3",
			versionPath:          []string{"status", "resource", "version"},
			additionalRegistry:   "oci-registry-docker-registry.registry.svc.cluster.local",
			additionalRepository: "platform-mesh/account-operator",
			additionalTag:        "1.2.3-localized",
			expectedName:         "test-resource",
			expectedNs:           "default",
			expectedPath:         []string{"spec", "values", "image", "tag"},
			expectedVersion:      "1.2.3-localized",
			expectedRegistry:     "oci-registry-docker-registry.registry.svc.cluster.local",
			expectedRepository:   "platform-mesh/account-operator",
		},
		{
			// Regression: a digest written by a previous reconcile must not survive
			// when the resource no longer carries coordinates, otherwise the stale
			// digest (which takes precedence over the tag) would pin the old image.
			name:            "stale coordinates cleared on tag-only fallback",
			repo:            "oci",
			artifact:        "image",
			resourceName:    "test-resource",
			resourceNs:      "default",
			version:         "2.0.0",
			versionPath:     []string{"status", "resource", "version"},
			existingCoords:  map[string]string{"registry": "stale-reg", "repository": "stale/repo", "digest": "sha256:stale"},
			expectedName:    "test-resource",
			expectedNs:      "default",
			expectedPath:    []string{"spec", "values", "image", "tag"},
			expectedVersion: "2.0.0",
		},
		{
			// Regression: fresh registry/repository without a digest must clear a
			// previously injected digest so tag + stale digest can never coexist.
			name:                 "stale digest cleared when new coordinates omit digest",
			repo:                 "oci",
			artifact:             "image",
			resourceName:         "test-resource",
			resourceNs:           "default",
			version:              "2.0.0",
			versionPath:          []string{"status", "resource", "version"},
			additionalRegistry:   "oci-registry-docker-registry.registry.svc.cluster.local",
			additionalRepository: "platform-mesh/account-operator",
			existingCoords:       map[string]string{"digest": "sha256:stale"},
			expectedName:         "test-resource",
			expectedNs:           "default",
			expectedPath:         []string{"spec", "values", "image", "tag"},
			expectedVersion:      "2.0.0",
			expectedRegistry:     "oci-registry-docker-registry.registry.svc.cluster.local",
			expectedRepository:   "platform-mesh/account-operator",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			ctx := s.T().Context()

			annotations := map[string]any{
				"artifact": tt.artifact,
				"repo":     tt.repo,
			}
			if tt.forAnnotation != "" {
				annotations["for"] = tt.forAnnotation
			}
			if tt.pathAnnotation != "" {
				annotations["path"] = tt.pathAnnotation
			}
			if tt.versionPathAnnotation != "" {
				annotations["version-path"] = tt.versionPathAnnotation
			}

			inst := &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "delivery.ocm.software/v1alpha1",
					"kind":       "Resource",
					"metadata": map[string]any{
						"name":        tt.resourceName,
						"namespace":   tt.resourceNs,
						"annotations": annotations,
					},
					"status": map[string]any{
						"resource": map[string]any{
							"version": tt.version,
						},
					},
					"spec": map[string]any{},
				},
			}

			if err := unstructured.SetNestedField(inst.Object, tt.version, tt.versionPath...); err != nil {
				s.Require().NoError(err)
			}

			if tt.additionalRegistry != "" {
				s.Require().NoError(unstructured.SetNestedField(inst.Object, tt.additionalRegistry, "status", "additional", "registry"))
			}
			if tt.additionalRepository != "" {
				s.Require().NoError(unstructured.SetNestedField(inst.Object, tt.additionalRepository, "status", "additional", "repository"))
			}
			if tt.additionalDigest != "" {
				s.Require().NoError(unstructured.SetNestedField(inst.Object, tt.additionalDigest, "status", "additional", "digest"))
			}
			if tt.additionalTag != "" {
				s.Require().NoError(unstructured.SetNestedField(inst.Object, tt.additionalTag, "status", "additional", "tag"))
			}

			clientMock := new(mocks.Client)
			subroutine := NewResourceSubroutine(clientMock, nil, nil)

			clientMock.On("List", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
			clientMock.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(
				func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					if _, ok := obj.(*corev1.ConfigMap); ok {
						return apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, "")
					}
					unstr := obj.(*unstructured.Unstructured)
					unstr.SetName(key.Name)
					unstr.SetNamespace(key.Namespace)
					unstr.Object["spec"] = map[string]any{"values": map[string]any{}}
					parentPath := tt.expectedPath[:len(tt.expectedPath)-1]
					for leaf, value := range tt.existingCoords {
						s.Require().NoError(unstructured.SetNestedField(unstr.Object, value, appendPath(parentPath, leaf)...))
					}
					return nil
				},
			)
			clientMock.EXPECT().Update(mock.Anything, mock.MatchedBy(func(obj ctrlruntimeclient.Object) bool {
				helmRelease, ok := obj.(*unstructured.Unstructured)
				if !ok {
					return false
				}
				if helmRelease.GetName() != tt.expectedName {
					return false
				}
				if helmRelease.GetNamespace() != tt.expectedNs {
					return false
				}
				actualVersion, found, err := unstructured.NestedString(helmRelease.Object, tt.expectedPath...)
				if err != nil || !found || actualVersion != tt.expectedVersion {
					return false
				}

				// A coordinate must equal its expected value, or be absent when none is expected.
				parentPath := tt.expectedPath[:len(tt.expectedPath)-1]
				checkCoord := func(leaf, expected string) bool {
					actual, found, _ := unstructured.NestedString(helmRelease.Object, appendPath(parentPath, leaf)...)
					if expected == "" {
						return !found
					}
					return found && actual == expected
				}
				return checkCoord("registry", tt.expectedRegistry) &&
					checkCoord("repository", tt.expectedRepository) &&
					checkCoord("digest", tt.expectedDigest)
			}), mock.Anything).Return(nil)

			result, err := subroutine.Process(ctx, inst)
			s.Nil(err)
			s.NotNil(result)
			// Without this the case also passes when the Update is never reached,
			// so a regression that skips the injection would go unnoticed.
			clientMock.AssertExpectations(s.T())
		})
	}
}

func (s *ResourceTestSuite) Test_updateHelmReleaseImage_StoresResolvedTag() {
	ctx := s.T().Context()
	store := subroutines.NewImageVersionStore()

	inst := imageResource("test-resource",
		map[string]any{"artifact": "image", "repo": "oci"},
		"1.2.3",
		map[string]any{
			"registry":   "oci-registry-docker-registry.registry.svc.cluster.local",
			"repository": "platform-mesh/account-operator",
			"tag":        "1.2.3-localized",
			"digest":     "sha256:cb5be99827d7cfa107fc7ca06f5b2fb0ea486f3ffb0315baf2be1bb348f9db77",
		})

	clientMock := new(mocks.Client)
	clientMock.On("List", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	expectEmptyHelmReleaseGet(clientMock)
	clientMock.EXPECT().Update(mock.Anything, mock.Anything, mock.Anything).Return(nil)

	subroutine := NewResourceSubroutine(clientMock, nil, store)
	_, err := subroutine.Process(ctx, inst)
	s.Require().NoError(err)

	stored := storeToMap(store, "default", "test-resource")
	s.Equal(map[string]string{
		"image.tag":        "1.2.3-localized",
		"image.registry":   "oci-registry-docker-registry.registry.svc.cluster.local",
		"image.repository": "platform-mesh/account-operator",
		"image.digest":     "sha256:cb5be99827d7cfa107fc7ca06f5b2fb0ea486f3ffb0315baf2be1bb348f9db77",
	}, stored)
}

func (s *ResourceTestSuite) Test_updateHelmReleaseImage_RemovesStaleCoordinatesFromStore() {
	ctx := s.T().Context()
	store := subroutines.NewImageVersionStore()
	store.Set("default", "test-resource", "image.registry", "stale-registry")
	store.Set("default", "test-resource", "image.repository", "stale/repo")
	store.Set("default", "test-resource", "image.digest", "sha256:stale")

	inst := imageResource("test-resource",
		map[string]any{"artifact": "image", "repo": "oci"},
		"2.0.0", nil)

	clientMock := new(mocks.Client)
	clientMock.On("List", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	expectEmptyHelmReleaseGet(clientMock)
	clientMock.EXPECT().Update(mock.Anything, mock.Anything, mock.Anything).Return(nil)

	subroutine := NewResourceSubroutine(clientMock, nil, store)
	_, err := subroutine.Process(ctx, inst)
	s.Require().NoError(err)

	stored := storeToMap(store, "default", "test-resource")
	s.Equal(map[string]string{"image.tag": "2.0.0"}, stored)
}

func (s *ResourceTestSuite) Test_updateHelmReleaseImage_StoresCoordinatesAtCustomPath() {
	ctx := s.T().Context()
	store := subroutines.NewImageVersionStore()

	inst := imageResource("test-resource",
		map[string]any{"artifact": "image", "repo": "oci", "path": "webhook.image.tag"},
		"1.2.3",
		map[string]any{
			"registry":   "registry.internal",
			"repository": "platform-mesh/webhook",
		})

	clientMock := new(mocks.Client)
	clientMock.On("List", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	expectEmptyHelmReleaseGet(clientMock)
	clientMock.EXPECT().Update(mock.Anything, mock.Anything, mock.Anything).Return(nil)

	subroutine := NewResourceSubroutine(clientMock, nil, store)
	_, err := subroutine.Process(ctx, inst)
	s.Require().NoError(err)

	stored := storeToMap(store, "default", "test-resource")
	s.Equal(map[string]string{
		"webhook.image.tag":        "1.2.3",
		"webhook.image.registry":   "registry.internal",
		"webhook.image.repository": "platform-mesh/webhook",
	}, stored)
}

func (s *ResourceTestSuite) Test_updateHelmReleaseImage_PathLeafCollision() {
	ctx := s.T().Context()

	inst := imageResource("test-resource",
		map[string]any{"artifact": "image", "repo": "oci", "path": "image.registry"},
		"1.2.3",
		map[string]any{
			"registry":   "oci-registry-docker-registry.registry.svc.cluster.local",
			"repository": "platform-mesh/account-operator",
		})

	clientMock := new(mocks.Client)
	clientMock.On("List", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	expectEmptyHelmReleaseGet(clientMock)
	clientMock.EXPECT().Update(mock.Anything, mock.MatchedBy(func(obj ctrlruntimeclient.Object) bool {
		helmRelease, ok := obj.(*unstructured.Unstructured)
		if !ok {
			return false
		}
		// The configured leaf (image.registry) holds the tag value.
		actual, found, _ := unstructured.NestedString(helmRelease.Object, "spec", "values", "image", "registry")
		if !found || actual != "1.2.3" {
			return false
		}
		// No coordinates injected as siblings.
		if _, foundRepo, _ := unstructured.NestedString(helmRelease.Object, "spec", "values", "image", "repository"); foundRepo {
			return false
		}
		if _, foundDigest, _ := unstructured.NestedString(helmRelease.Object, "spec", "values", "image", "digest"); foundDigest {
			return false
		}
		return true
	}), mock.Anything).Return(nil)

	subroutine := NewResourceSubroutine(clientMock, nil, nil)
	result, err := subroutine.Process(ctx, inst)
	s.Nil(err)
	s.NotNil(result)
}

func (s *ResourceTestSuite) Test_updateHelmReleaseImage_CombinedRefStyle() {
	ctx := s.T().Context()
	store := subroutines.NewImageVersionStore()

	inst := imageResource("openfga-image",
		map[string]any{
			"artifact":  "image",
			"repo":      "oci",
			"for":       "openfga",
			"image-ref": "combined",
		},
		"1.11.2",
		map[string]any{
			"registry":   "zot.local/platform-mesh",
			"repository": "openfga/openfga",
			"tag":        "1.11.2",
			"digest":     "sha256:cb5be99827d7cfa107fc7ca06f5b2fb0ea486f3ffb0315baf2be1bb348f9db77",
		})

	clientMock := new(mocks.Client)
	clientMock.On("List", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	expectEmptyHelmReleaseGet(clientMock)
	clientMock.EXPECT().Update(mock.Anything, mock.MatchedBy(func(obj ctrlruntimeclient.Object) bool {
		hr, ok := obj.(*unstructured.Unstructured)
		if !ok {
			return false
		}
		repo, foundRepo, _ := unstructured.NestedString(hr.Object, "spec", "values", "image", "repository")
		if !foundRepo || repo != "zot.local/platform-mesh/openfga/openfga" {
			return false
		}
		tag, foundTag, _ := unstructured.NestedString(hr.Object, "spec", "values", "image", "tag")
		if !foundTag || tag != "1.11.2" {
			return false
		}
		// registry and digest must NOT be written (chart schema rejects them).
		if _, foundReg, _ := unstructured.NestedString(hr.Object, "spec", "values", "image", "registry"); foundReg {
			return false
		}
		if _, foundDigest, _ := unstructured.NestedString(hr.Object, "spec", "values", "image", "digest"); foundDigest {
			return false
		}
		return true
	}), mock.Anything).Return(nil)

	subroutine := NewResourceSubroutine(clientMock, nil, store)
	_, err := subroutine.Process(ctx, inst)
	s.Require().NoError(err)

	stored := storeToMap(store, "default", "openfga")
	s.Equal(map[string]string{
		"image.tag":        "1.11.2",
		"image.repository": "zot.local/platform-mesh/openfga/openfga",
	}, stored)
}

func (s *ResourceTestSuite) Test_updateHelmReleaseImage_GetError() {
	ctx := s.T().Context()

	inst := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "delivery.ocm.software/v1alpha1",
			"kind":       "Resource",
			"metadata": map[string]any{
				"name":      "test-resource",
				"namespace": "default",
				"annotations": map[string]any{
					"artifact": "image",
					"repo":     "helm",
				},
			},
			"status": map[string]any{
				"resource": map[string]any{
					"version": "1.2.3",
				},
			},
			"spec": map[string]any{},
		},
	}

	clientMock := new(mocks.Client)
	subroutine := NewResourceSubroutine(clientMock, nil, nil)

	clientMock.On("List", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	// The profile ConfigMap must resolve, otherwise getAppNamespaceFromProfile fails
	// first and Process never reaches updateHelmReleaseImage — the case this asserts.
	clientMock.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			if _, ok := obj.(*corev1.ConfigMap); ok {
				return apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, "")
			}
			return errors.New("get error")
		},
	)

	result, err := subroutine.Process(ctx, inst)
	s.Require().Error(err)
	s.Contains(err.Error(), "HelmRelease default/test-resource not found")
	s.Contains(err.Error(), "get error")
	s.NotNil(result)
}

func (s *ResourceTestSuite) Test_updateHelmReleaseImage_UpdateError() {
	ctx := s.T().Context()

	inst := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "delivery.ocm.software/v1alpha1",
			"kind":       "Resource",
			"metadata": map[string]any{
				"name":      "test-resource",
				"namespace": "default",
				"annotations": map[string]any{
					"artifact": "image",
					"repo":     "helm",
				},
			},
			"status": map[string]any{
				"resource": map[string]any{
					"version": "1.2.3",
				},
			},
			"spec": map[string]any{},
		},
	}

	clientMock := new(mocks.Client)
	subroutine := NewResourceSubroutine(clientMock, nil, nil)

	clientMock.On("List", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	expectEmptyHelmReleaseGet(clientMock)
	clientMock.EXPECT().Update(mock.Anything, mock.Anything, mock.Anything).Return(errors.New("update error"))

	result, err := subroutine.Process(ctx, inst)
	s.NotNil(err)
	s.NotNil(result)
}

// expectEmptyHelmReleaseGet sets up the client mock to return a HelmRelease with an
// empty spec.values, so updateHelmReleaseImage has a clean object to inject into.
func expectEmptyHelmReleaseGet(clientMock *mocks.Client) {
	clientMock.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			if _, ok := obj.(*corev1.ConfigMap); ok {
				return apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, "")
			}
			unstr := obj.(*unstructured.Unstructured)
			unstr.SetName(key.Name)
			unstr.SetNamespace(key.Namespace)
			unstr.Object["spec"] = map[string]any{"values": map[string]any{}}
			return nil
		},
	)
}

// storeToMap collects an ImageVersionStore's entries for an app into a path->version map.
func storeToMap(store *subroutines.ImageVersionStore, namespace, name string) map[string]string {
	stored := map[string]string{}
	for _, iv := range store.Get(namespace, name) {
		stored[iv.Path] = iv.Version
	}
	return stored
}

// imageResource builds the minimal Resource the image-injection tests reconcile:
// status.resource.version plus, when given, coordinates under status.additional.
func imageResource(name string, annotations map[string]any, version string, additional map[string]any) *unstructured.Unstructured {
	status := map[string]any{
		"resource": map[string]any{"version": version},
	}
	if additional != nil {
		status["additional"] = additional
	}
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "delivery.ocm.software/v1alpha1",
			"kind":       "Resource",
			"metadata": map[string]any{
				"name":        name,
				"namespace":   "default",
				"annotations": annotations,
			},
			"status": status,
			"spec":   map[string]any{},
		},
	}
}
