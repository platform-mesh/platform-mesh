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
	WebhookValidations = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "account_operator_webhook_validations_total",
			Help: "Total number of Account webhook validation requests.",
		},
		[]string{"operation", "result", "account_type"},
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
		WebhookValidations,
		WorkspaceReadyDuration,
		WorkspaceTypeOperations,
		OrgProvisioningDuration,
	)
}
