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

package sentry

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	pmerrors "go.platform-mesh.io/golang-commons/errors"
)

func TestIsTerminatingNSError(t *testing.T) {
	gvr := schema.GroupResource{
		Group:    "a.api.group",
		Resource: "aResource",
	}

	conflictError := errors.NewConflict(gvr, "aName", fmt.Errorf("aMessage"))
	conflictError.ErrStatus.Details.Causes = []metav1.StatusCause{{Type: v1.NamespaceTerminatingCause}}
	isTerminating := isTerminatingNSError(conflictError)
	assert.True(t, isTerminating)
}

func TestShouldBeProcessedNegative(t *testing.T) {
	gvr := schema.GroupResource{
		Group:    "a.api.group",
		Resource: "aResource",
	}

	conflictError := errors.NewConflict(gvr, "aName", fmt.Errorf("aMessage"))
	conflictError.ErrStatus.Details.Causes = []metav1.StatusCause{{Type: v1.NamespaceTerminatingCause}}
	shouldBeProcessed := ShouldBeProcessed(conflictError)
	assert.False(t, shouldBeProcessed)
}

func TestShouldBeProcessedPositive(t *testing.T) {
	gvr := schema.GroupResource{
		Group:    "a.api.group",
		Resource: "aResource",
	}

	conflictError := errors.NewConflict(gvr, "aName", fmt.Errorf("aMessage"))
	shouldBeProcessed := ShouldBeProcessed(conflictError)
	assert.True(t, shouldBeProcessed)
}

func TestShouldBeProcessedSentryPositive(t *testing.T) {
	err := pmerrors.New("test error")
	sentryError := SentryError(err)
	shouldBeProcessed := ShouldBeProcessed(sentryError)
	assert.True(t, shouldBeProcessed)
}
