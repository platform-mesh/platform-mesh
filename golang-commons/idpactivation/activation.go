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

// Package idpactivation validates administrator-approved coordinated identity
// activation. A record is an audited authorization, not a replacement for the
// migration executor's maintenance, fencing and participant verification.
package idpactivation

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kcp-dev/logicalcluster/v3"
)

const (
	Version = "identity.platform-mesh.io/activation/v1"
	DataKey = "activation.json"
)

var participants = []string{"orchestration", "iam", "fga-writer", "fga-authorizer", "portal", "welcome"}

type Binding struct {
	InstallationID            string    `json:"installationId"`
	OrganizationCluster       string    `json:"organizationCluster"`
	ProviderClassUID          types.UID `json:"providerClassUID"`
	ConfigurationRevision     string    `json:"configurationRevision"`
	ClientConfigurationDigest string    `json:"clientConfigurationDigest"`
}

type Participant struct {
	Protocol       string `json:"protocol"`
	Epoch          string `json:"epoch"`
	ArtifactDigest string `json:"artifactDigest"`
	Prepared       bool   `json:"prepared"`
}

type Record struct {
	Version string  `json:"version"`
	Binding Binding `json:"binding"`
	Epoch   string  `json:"epoch"`
	Phase   string  `json:"phase"`

	ApprovedBy         string                 `json:"approvedBy"`
	ApprovedAt         metav1.Time            `json:"approvedAt"`
	InventoryDigest    string                 `json:"inventoryDigest"`
	MappingDigest      string                 `json:"mappingDigest"`
	FencingDigest      string                 `json:"fencingDigest"`
	PredecessorsFenced bool                   `json:"predecessorsFenced"`
	CanonicalDataReady bool                   `json:"canonicalDataReady"`
	Participants       map[string]Participant `json:"participants"`
}

type Permit struct {
	Epoch           string
	RecordUID       types.UID
	ResourceVersion string
}

type Gate interface {
	Check(context.Context, Binding) (Permit, error)
}

// SecretGate reads only the installation-selected private Secret. Only trusted
// migration administrators may write it; tenant API authors must have no access.
type SecretGate struct {
	Reader          ctrlruntimeclient.Reader
	Namespace, Name string
}

func Digest(value string) bool {
	raw, found := strings.CutPrefix(value, "sha256:")
	if !found || len(raw) != 64 || raw != strings.ToLower(raw) {
		return false
	}
	_, err := hex.DecodeString(raw)
	return err == nil
}

func Validate(record Record, binding Binding, now time.Time) error {
	if record.Version != Version || record.Phase != "Active" || record.Epoch == "" ||
		record.Binding != binding || binding.InstallationID == "" || binding.ProviderClassUID == "" ||
		binding.ConfigurationRevision == "" || !Digest(binding.ClientConfigurationDigest) ||
		!logicalcluster.Name(binding.OrganizationCluster).IsValid() {
		return errors.New("identity activation is absent, inactive or belongs to a different installation/provider")
	}
	if record.ApprovedBy == "" || record.ApprovedAt.IsZero() || record.ApprovedAt.After(now.Add(5*time.Minute)) ||
		!record.PredecessorsFenced || !record.CanonicalDataReady ||
		!Digest(record.InventoryDigest) || !Digest(record.MappingDigest) || !Digest(record.FencingDigest) {
		return errors.New("identity activation requires audited approval, inventory, mappings and predecessor fencing")
	}
	if len(record.Participants) != len(participants) {
		return errors.New("identity activation participant set is incomplete")
	}
	for _, name := range participants {
		participant, found := record.Participants[name]
		if !found || participant.Protocol != Version || participant.Epoch != record.Epoch ||
			!participant.Prepared || !Digest(participant.ArtifactDigest) {
			return errors.New("identity activation participants are not prepared for the approved epoch")
		}
	}
	return nil
}

func (g *SecretGate) Check(ctx context.Context, binding Binding) (Permit, error) {
	if g.Reader == nil || g.Namespace == "" || g.Name == "" {
		return Permit{}, errors.New("private identity activation reader and fixed Secret location are required")
	}
	secret := &corev1.Secret{}
	if err := g.Reader.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: g.Namespace, Name: g.Name}, secret); err != nil {
		return Permit{}, err
	}
	if secret.UID == "" || secret.ResourceVersion == "" || !secret.DeletionTimestamp.IsZero() {
		return Permit{}, errors.New("identity activation record has no live immutable identity")
	}
	data := secret.Data[DataKey]
	if len(data) == 0 || len(data) > 64<<10 {
		return Permit{}, errors.New("identity activation record is missing or oversized")
	}
	var record Record
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil || decoder.Decode(new(any)) != io.EOF {
		return Permit{}, errors.New("identity activation record is malformed")
	}
	if err := Validate(record, binding, time.Now()); err != nil {
		return Permit{}, err
	}
	return Permit{Epoch: record.Epoch, RecordUID: secret.UID, ResourceVersion: secret.ResourceVersion}, nil
}
