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

package spread

import (
	"fmt"
	"math/rand/v2"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// RefreshLabel triggers an immediate reconciliation when present.
	RefreshLabel = "platform-mesh.io/refresh-reconcile"

	defaultMinDuration = 12 * time.Hour
	defaultMaxDuration = 24 * time.Hour
)

// Option configures the spread Manager.
type Option func(*Manager)

// WithMinDuration sets the minimum spread duration.
func WithMinDuration(d time.Duration) Option {
	return func(m *Manager) { m.minDuration = d }
}

// WithMaxDuration sets the maximum spread duration.
func WithMaxDuration(d time.Duration) Option {
	return func(m *Manager) { m.maxDuration = d }
}

// Manager implements reconciliation spreading to avoid thundering-herd effects.
type Manager struct {
	minDuration time.Duration
	maxDuration time.Duration
}

// NewManager creates a new spread Manager with the given options.
// Panics if minDuration > maxDuration after applying options.
func NewManager(opts ...Option) *Manager {
	m := &Manager{
		minDuration: defaultMinDuration,
		maxDuration: defaultMaxDuration,
	}
	for _, opt := range opts {
		opt(m)
	}
	if m.minDuration > m.maxDuration {
		panic(fmt.Sprintf("spread: minDuration (%s) must not exceed maxDuration (%s)", m.minDuration, m.maxDuration))
	}
	return m
}

// ReconcileRequired returns true if the object should be reconciled now.
func (m *Manager) ReconcileRequired(obj client.Object) bool {
	s, ok := obj.(SpreadReconcileStatus)
	if !ok {
		return true
	}

	// Generation changed — always reconcile.
	if obj.GetGeneration() != s.GetObservedGeneration() {
		return true
	}

	// Refresh label present — always reconcile.
	if _, ok := obj.GetLabels()[RefreshLabel]; ok {
		return true
	}

	// Past next reconcile time.
	nrt := s.GetNextReconcileTime()
	if nrt.IsZero() {
		return true
	}

	return time.Now().After(nrt.Time)
}

// RequeueDelay returns the remaining time until the next scheduled reconciliation.
func (m *Manager) RequeueDelay(obj client.Object) time.Duration {
	s, ok := obj.(SpreadReconcileStatus)
	if !ok {
		return 0
	}

	nrt := s.GetNextReconcileTime()
	if nrt.IsZero() {
		return 0
	}

	remaining := time.Until(nrt.Time)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// SetNextReconcileTime sets a random next reconcile time within the configured range.
func (m *Manager) SetNextReconcileTime(obj client.Object) {
	s, ok := obj.(SpreadReconcileStatus)
	if !ok {
		return
	}

	var jitter time.Duration
	if jitterRange := m.maxDuration - m.minDuration; jitterRange > 0 {
		jitter = time.Duration(rand.Int64N(int64(jitterRange)))
	}
	next := time.Now().Add(m.minDuration + jitter)
	s.SetNextReconcileTime(metav1.NewTime(next))
}

// UpdateObservedGeneration sets the observed generation to the current generation.
func (m *Manager) UpdateObservedGeneration(obj client.Object) {
	s, ok := obj.(SpreadReconcileStatus)
	if !ok {
		return
	}
	s.SetObservedGeneration(obj.GetGeneration())
}

// RemoveRefreshLabel removes the refresh label and returns true if it was present.
func (m *Manager) RemoveRefreshLabel(obj client.Object) bool {
	labels := obj.GetLabels()
	if labels == nil {
		return false
	}
	if _, ok := labels[RefreshLabel]; !ok {
		return false
	}
	delete(labels, RefreshLabel)
	obj.SetLabels(labels)
	return true
}
