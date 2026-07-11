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

package e2e

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	pmbrokerv1alpha1 "go.platform-mesh.io/apis/broker/v1alpha1"
	examplev1alpha1 "go.platform-mesh.io/resource-broker/api/example/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"
)

// TestManagerCopy only tests that the manager can copy from a source to
// a destination cluster.
func TestManagerCopy(t *testing.T) {
	t.Parallel()

	frame := NewFrame(t)
	provider := frame.NewProvider(t, "provider")
	consumer := frame.NewConsumer(t, "consumer")

	t.Log("Create AcceptAPI in provider workspace")
	err := provider.Client.Create(
		t.Context(),
		&pmbrokerv1alpha1.AcceptAPI{
			ObjectMeta: metav1.ObjectMeta{
				Name: "accept-certificates",
			},
			Spec: pmbrokerv1alpha1.AcceptAPISpec{
				GVR: metav1.GroupVersionResource{
					Group:    "example.platform-mesh.io",
					Version:  "v1alpha1",
					Resource: "certificates",
				},
				APIExportName: exampleExportName,
			},
		},
	)
	require.NoError(t, err)

	frame.StartBroker(t)

	namespace := "default" //nolint:goconst,nolintlint
	certName := "test-certificate"

	t.Log("Create Certificate in consumer workspace")
	err = consumer.Client.Create(
		t.Context(),
		&examplev1alpha1.Certificate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      certName,
				Namespace: namespace,
			},
			Spec: examplev1alpha1.CertificateSpec{
				FQDN: ptr.To("example.com"),
			},
		},
	)
	require.NoError(t, err)

	t.Log("Wait for Certificate to appear in staging workspace")
	stagingClient := frame.StagingClient(t, provider)
	require.Eventually(t, func() bool {
		cert := &examplev1alpha1.Certificate{}
		err := stagingClient.Get(
			t.Context(),
			types.NamespacedName{
				Name:      certName,
				Namespace: namespace,
			},
			cert,
		)
		if err != nil {
			t.Logf("error getting certificate from staging workspace: %v", err)
			return false
		}
		return ptr.Deref(cert.Spec.FQDN, "") == "example.com"
	}, wait.ForeverTestTimeout, time.Second)
}
