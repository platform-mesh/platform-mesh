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

package idpactivation

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestActivationRequiresTheWholeApprovedAndFencedParticipantSet(t *testing.T) {
	record := activationFixture()
	require.NoError(t, Validate(record, record.Binding, record.ApprovedAt.Time))
	for _, edit := range []func(*Record){
		func(r *Record) { r.PredecessorsFenced = false },
		func(r *Record) { r.CanonicalDataReady = false },
		func(r *Record) { r.Phase = "Prepared" },
		func(r *Record) { r.Binding.ProviderClassUID = "replacement-class" },
		func(r *Record) { r.Binding.ClientConfigurationDigest = "" },
		func(r *Record) { r.MappingDigest = "" },
		func(r *Record) { delete(r.Participants, "portal") },
		func(r *Record) {
			participant := r.Participants["fga-authorizer"]
			participant.Epoch = "previous-epoch"
			r.Participants["fga-authorizer"] = participant
		},
	} {
		candidate := activationFixture()
		edit(&candidate)
		require.Error(t, Validate(candidate, record.Binding, record.ApprovedAt.Time))
	}
}

func TestSecretActivationCannotSelectTenantSecretsOrAcceptMalformedRecords(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	record := activationFixture()
	data, err := json.Marshal(record)
	require.NoError(t, err)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "activation", Namespace: "private", UID: "approval-uid"},
		Data:       map[string][]byte{DataKey: data},
	}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
	gate := &SecretGate{Reader: client, Namespace: "private", Name: "activation"}
	permit, err := gate.Check(t.Context(), record.Binding)
	require.NoError(t, err)
	require.Equal(t, "approval-uid", string(permit.RecordUID))
	require.Equal(t, record.Epoch, permit.Epoch)
	changedClientBinding := record.Binding
	changedClientBinding.ClientConfigurationDigest = "sha256:" + strings.Repeat("b", 64)
	_, err = gate.Check(t.Context(), changedClientBinding)
	require.Error(t, err)
	gate.Namespace = "tenant-selected"
	_, err = gate.Check(t.Context(), record.Binding)
	require.Error(t, err)
	gate.Namespace = "private"
	require.NoError(t, client.Get(t.Context(), ctrlruntimeclient.ObjectKeyFromObject(secret), secret))
	secret.Data[DataKey] = append(data, []byte(` {}`)...)
	require.NoError(t, client.Update(t.Context(), secret))
	_, err = gate.Check(t.Context(), record.Binding)
	require.ErrorContains(t, err, "malformed")
}

func activationFixture() Record {
	digest := "sha256:" + strings.Repeat("a", 64)
	record := Record{
		Version: Version, Binding: Binding{
			InstallationID: "installation", OrganizationCluster: "orgs-cluster",
			ProviderClassUID: "class-uid", ConfigurationRevision: "config-v1",
			ClientConfigurationDigest: digest,
		},
		Epoch: "approved-epoch", Phase: "Active", ApprovedBy: "administrator", ApprovedAt: metav1.Now(),
		InventoryDigest: digest, MappingDigest: digest, FencingDigest: digest,
		PredecessorsFenced: true, CanonicalDataReady: true, Participants: map[string]Participant{},
	}
	for _, name := range participants {
		record.Participants[name] = Participant{Protocol: Version, Epoch: record.Epoch, ArtifactDigest: digest, Prepared: true}
	}
	return record
}
