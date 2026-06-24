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

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	AccountsReconciled = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "account_operator_accounts_reconciled_total",
			Help: "Total number of Account reconcile calls by type and result.",
		},
		[]string{"type", "result"},
	)
	WorkspaceReadyDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "account_operator_workspace_ready_duration_seconds",
			Help:    "Time from Account creation to Workspace reaching Ready phase.",
			Buckets: []float64{1, 5, 10, 30, 60, 120, 300},
		},
		[]string{"type"},
	)
	WorkspaceTypeOperations = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "account_operator_workspacetype_operations_total",
			Help: "Total number of WorkspaceType create or update operations performed for organizations.",
		},
		[]string{"operation", "wst_kind"},
	)
	OrgProvisioningDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "account_operator_org_provisioning_duration_seconds",
			Help:    "Time from organization Account creation to its AccountInfo being written.",
			Buckets: []float64{1, 5, 10, 30, 60, 120, 300},
		},
		[]string{},
	)
)

func init() {
	ctrlmetrics.Registry.MustRegister(
		AccountsReconciled,
		WorkspaceReadyDuration,
		WorkspaceTypeOperations,
		OrgProvisioningDuration,
	)
}
