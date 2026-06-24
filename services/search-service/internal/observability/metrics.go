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

package observability

import (
	"sync/atomic"
	"time"
)

type Metrics struct {
	searchRequests        atomic.Int64
	searchDurationNanos   atomic.Int64
	openSearchCalls       atomic.Int64
	openFGACalls          atomic.Int64
	droppedMissingContext atomic.Int64
	authDenied            atomic.Int64
}

func NewMetrics() *Metrics {
	return &Metrics{}
}

func (m *Metrics) IncSearchRequests() {
	m.searchRequests.Add(1)
}

func (m *Metrics) ObserveSearchDuration(d time.Duration) {
	m.searchDurationNanos.Add(d.Nanoseconds())
}

func (m *Metrics) AddOpenSearchCalls(n int) {
	m.openSearchCalls.Add(int64(n))
}

func (m *Metrics) AddOpenFGACalls(n int) {
	m.openFGACalls.Add(int64(n))
}

func (m *Metrics) AddDroppedMissingContext(n int) {
	m.droppedMissingContext.Add(int64(n))
}

func (m *Metrics) AddAuthDenied(n int) {
	m.authDenied.Add(int64(n))
}

type Snapshot struct {
	SearchRequests        int64
	SearchDurationNanos   int64
	OpenSearchCalls       int64
	OpenFGACalls          int64
	DroppedMissingContext int64
	AuthDenied            int64
}

func (m *Metrics) Snapshot() Snapshot {
	return Snapshot{
		SearchRequests:        m.searchRequests.Load(),
		SearchDurationNanos:   m.searchDurationNanos.Load(),
		OpenSearchCalls:       m.openSearchCalls.Load(),
		OpenFGACalls:          m.openFGACalls.Load(),
		DroppedMissingContext: m.droppedMissingContext.Load(),
		AuthDenied:            m.authDenied.Load(),
	}
}
