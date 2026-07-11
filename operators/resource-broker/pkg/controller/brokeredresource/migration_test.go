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

package brokeredresource

import (
	"context"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmbrokerv1alpha1 "go.platform-mesh.io/apis/broker/v1alpha1"
	pmcoordbrokerv1alpha1 "go.platform-mesh.io/apis/coordbroker/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	testOtherProviderCluster = "other-provider-cluster"
	testOtherAcceptAPIName   = "accept-widgets-2"
	testOtherExportName      = "other-export"
)

// testTargetStagingName is the staging workspace name for the migration
// target provider.
var testTargetStagingName = stagingWorkspaceName(testConsumerCluster, testOtherProviderCluster, testOtherExportName)

// migrationTestClients bundles the fake clients used by tests exercising
// migration behavior.
type migrationTestClients struct {
	coordination ctrlruntimeclient.Client
	provider     ctrlruntimeclient.Client
	staging      ctrlruntimeclient.Client
	target       ctrlruntimeclient.Client
}

// testMigrationOptions returns Options with a WorkspaceClientFunc that
// dispatches to the provider, assigned staging and migration target clients.
func testMigrationOptions(t *testing.T, clients *migrationTestClients, refs []AcceptAPIRef) Options {
	t.Helper()

	return Options{
		GVK:                testGVK,
		GVR:                testGVR,
		StagingTreeRoot:    testTreeRoot,
		CoordinationClient: clients.coordination,
		RequeueInterval:    DefaultRequeueInterval,
		WorkspaceClientFunc: func(path string) (ctrlruntimeclient.Client, error) {
			switch path {
			case testProviderCluster:
				return clients.provider, nil
			case testTreeRoot + ":" + testStagingName:
				return clients.staging, nil
			case testTreeRoot + ":" + testTargetStagingName:
				return clients.target, nil
			default:
				t.Fatalf("unexpected workspace client path %q", path)
				return nil, nil
			}
		},
		ListAcceptAPIs: func(_ context.Context) ([]AcceptAPIRef, error) {
			return refs, nil
		},
		PickAcceptAPI: func(refs []AcceptAPIRef) AcceptAPIRef {
			return refs[0]
		},
	}
}

// testMigrationName returns the deterministic Migration name for the test
// consumer object.
func testMigrationName() string {
	return migrationName(testConsumerCluster, testGVR, testNamespace, testResourceName)
}

// testMigration returns a Migration from the test provider to the other
// provider in the given state.
func testMigration(state pmcoordbrokerv1alpha1.MigrationState) *pmcoordbrokerv1alpha1.Migration {
	gvk := metav1.GroupVersionKind{
		Group:   testGVK.Group,
		Version: testGVK.Version,
		Kind:    testGVK.Kind,
	}

	return &pmcoordbrokerv1alpha1.Migration{
		ObjectMeta: metav1.ObjectMeta{
			Name: testMigrationName(),
		},
		Spec: pmcoordbrokerv1alpha1.MigrationSpec{
			Assignment:           testAssignmentName(),
			Namespace:            testNamespace,
			Name:                 testResourceName,
			FromStagingWorkspace: testStagingName,
			StagingWorkspace:     testTargetStagingName,
			From: pmcoordbrokerv1alpha1.MigrationTarget{
				GVK:             gvk,
				ProviderCluster: testProviderCluster,
				AcceptAPIName:   testAcceptAPIName,
			},
			To: pmcoordbrokerv1alpha1.MigrationTarget{
				GVK:             gvk,
				ProviderCluster: testOtherProviderCluster,
				AcceptAPIName:   testOtherAcceptAPIName,
			},
		},
		Status: pmcoordbrokerv1alpha1.MigrationStatus{
			State: state,
		},
	}
}

// testTargetStagingWorkspace returns the StagingWorkspace for the migration
// target provider in the given phase.
func testTargetStagingWorkspace(phase pmcoordbrokerv1alpha1.StagingWorkspacePhase) *pmcoordbrokerv1alpha1.StagingWorkspace {
	return &pmcoordbrokerv1alpha1.StagingWorkspace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testTargetStagingName,
		},
		Spec: pmcoordbrokerv1alpha1.StagingWorkspaceSpec{
			ConsumerCluster: testConsumerCluster,
			ProviderCluster: testOtherProviderCluster,
			APIExportName:   testOtherExportName,
		},
		Status: pmcoordbrokerv1alpha1.StagingWorkspaceStatus{
			Phase: phase,
		},
	}
}

// testOtherAcceptAPIRef returns an AcceptAPIRef for the other provider
// accepting the test GVR.
func testOtherAcceptAPIRef() AcceptAPIRef {
	return AcceptAPIRef{
		Cluster: testOtherProviderCluster,
		AcceptAPI: &pmbrokerv1alpha1.AcceptAPI{
			ObjectMeta: metav1.ObjectMeta{
				Name: testOtherAcceptAPIName,
			},
			Spec: pmbrokerv1alpha1.AcceptAPISpec{
				GVR:           testGVR,
				APIExportName: testOtherExportName,
			},
		},
	}
}

// testNonApplyingAcceptAPI returns an AcceptAPI whose GVR does not match the
// test consumer object.
func testNonApplyingAcceptAPI() *pmbrokerv1alpha1.AcceptAPI {
	acceptAPI := testAcceptAPI()
	acceptAPI.Spec.GVR.Resource = "gadgets"
	return acceptAPI
}

func TestMigrationName(t *testing.T) {
	t.Parallel()

	name := migrationName(testConsumerCluster, testGVR, testNamespace, testResourceName)
	assert.Regexp(t, regexp.MustCompile(`^migration-[0-9a-f]{16}$`), name)

	require.Equal(t, name, migrationName(testConsumerCluster, testGVR, testNamespace, testResourceName))
}
