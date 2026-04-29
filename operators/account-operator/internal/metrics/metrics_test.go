package metrics_test

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/suite"

	"github.com/platform-mesh/account-operator/internal/metrics"
)

type MetricsTestSuite struct {
	suite.Suite
}

func TestMetricsTestSuite(t *testing.T) {
	suite.Run(t, new(MetricsTestSuite))
}

// TestAccountsReconciled verifies that the AccountsReconciled counter increments
// correctly for each label combination (type, result). It captures the value
// before and after a simulated increment and asserts the counter increased by 1.
func (s *MetricsTestSuite) TestAccountsReconciled() {
	before := testutil.ToFloat64(metrics.AccountsReconciled.WithLabelValues("org", "success"))
	metrics.AccountsReconciled.WithLabelValues("org", "success").Inc()
	s.Require().Equal(before+1, testutil.ToFloat64(metrics.AccountsReconciled.WithLabelValues("org", "success")))

	before = testutil.ToFloat64(metrics.AccountsReconciled.WithLabelValues("org", "error"))
	metrics.AccountsReconciled.WithLabelValues("org", "error").Inc()
	s.Require().Equal(before+1, testutil.ToFloat64(metrics.AccountsReconciled.WithLabelValues("org", "error")))

	before = testutil.ToFloat64(metrics.AccountsReconciled.WithLabelValues("account", "requeue"))
	metrics.AccountsReconciled.WithLabelValues("account", "requeue").Inc()
	s.Require().Equal(before+1, testutil.ToFloat64(metrics.AccountsReconciled.WithLabelValues("account", "requeue")))
}

// TestWebhookValidations verifies that the WebhookValidations counter increments
// correctly for each label combination (operation, result, account_type). It covers
// allowed and denied outcomes for both create and update webhook calls.
func (s *MetricsTestSuite) TestWebhookValidations() {
	before := testutil.ToFloat64(metrics.WebhookValidations.WithLabelValues("create", "allowed", "org"))
	metrics.WebhookValidations.WithLabelValues("create", "allowed", "org").Inc()
	s.Require().Equal(before+1, testutil.ToFloat64(metrics.WebhookValidations.WithLabelValues("create", "allowed", "org")))

	before = testutil.ToFloat64(metrics.WebhookValidations.WithLabelValues("create", "denied", "org"))
	metrics.WebhookValidations.WithLabelValues("create", "denied", "org").Inc()
	s.Require().Equal(before+1, testutil.ToFloat64(metrics.WebhookValidations.WithLabelValues("create", "denied", "org")))

	before = testutil.ToFloat64(metrics.WebhookValidations.WithLabelValues("update", "allowed", "account"))
	metrics.WebhookValidations.WithLabelValues("update", "allowed", "account").Inc()
	s.Require().Equal(before+1, testutil.ToFloat64(metrics.WebhookValidations.WithLabelValues("update", "allowed", "account")))
}

// TestWorkspaceReadyDuration verifies that the WorkspaceReadyDuration histogram
// records observations. It checks that the number of collected series increases
// after observing durations for both org and account workspace types.
func (s *MetricsTestSuite) TestWorkspaceReadyDuration() {
	before := testutil.CollectAndCount(metrics.WorkspaceReadyDuration)
	metrics.WorkspaceReadyDuration.WithLabelValues("org").Observe(12.5)
	metrics.WorkspaceReadyDuration.WithLabelValues("org").Observe(45.0)
	metrics.WorkspaceReadyDuration.WithLabelValues("account").Observe(8.3)
	s.Assert().Greater(testutil.CollectAndCount(metrics.WorkspaceReadyDuration), before)
}

// TestOrgProvisioningDuration verifies that the OrgProvisioningDuration histogram
// records observations. It checks that the number of collected series increases
// after observing two provisioning durations.
func (s *MetricsTestSuite) TestOrgProvisioningDuration() {
	before := testutil.CollectAndCount(metrics.OrgProvisioningDuration)
	metrics.OrgProvisioningDuration.WithLabelValues().Observe(30.0)
	metrics.OrgProvisioningDuration.WithLabelValues().Observe(75.0)
	s.Assert().Greater(testutil.CollectAndCount(metrics.OrgProvisioningDuration), before)
}

// TestWorkspaceTypeOperations verifies that the WorkspaceTypeOperations counter
// increments correctly for each label combination (operation, wst_kind). It covers
// created and updated operations for both org and account workspace types.
func (s *MetricsTestSuite) TestWorkspaceTypeOperations() {
	before := testutil.ToFloat64(metrics.WorkspaceTypeOperations.WithLabelValues("created", "org"))
	metrics.WorkspaceTypeOperations.WithLabelValues("created", "org").Inc()
	s.Require().Equal(before+1, testutil.ToFloat64(metrics.WorkspaceTypeOperations.WithLabelValues("created", "org")))

	before = testutil.ToFloat64(metrics.WorkspaceTypeOperations.WithLabelValues("created", "account"))
	metrics.WorkspaceTypeOperations.WithLabelValues("created", "account").Inc()
	s.Require().Equal(before+1, testutil.ToFloat64(metrics.WorkspaceTypeOperations.WithLabelValues("created", "account")))

	before = testutil.ToFloat64(metrics.WorkspaceTypeOperations.WithLabelValues("updated", "org"))
	metrics.WorkspaceTypeOperations.WithLabelValues("updated", "org").Inc()
	s.Require().Equal(before+1, testutil.ToFloat64(metrics.WorkspaceTypeOperations.WithLabelValues("updated", "org")))
}
