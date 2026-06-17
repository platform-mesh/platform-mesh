package metrics_test

import (
	"testing"

	"github.com/platform-mesh/platform-mesh/operators/security-operator/internal/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/suite"
)

type MetricsTestSuite struct {
	suite.Suite
}

func TestMetricsTestSuite(t *testing.T) {
	suite.Run(t, new(MetricsTestSuite))
}

// TestReconcileTotal verifies that the ReconcileTotal counter increments
// correctly for each controller/result label combination.
func (s *MetricsTestSuite) TestReconcileTotal() {
	before := testutil.ToFloat64(metrics.ReconcileTotal.WithLabelValues("store", "success"))
	metrics.ReconcileTotal.WithLabelValues("store", "success").Inc()
	s.Require().Equal(before+1, testutil.ToFloat64(metrics.ReconcileTotal.WithLabelValues("store", "success")))

	before = testutil.ToFloat64(metrics.ReconcileTotal.WithLabelValues("invite", "error"))
	metrics.ReconcileTotal.WithLabelValues("invite", "error").Inc()
	s.Require().Equal(before+1, testutil.ToFloat64(metrics.ReconcileTotal.WithLabelValues("invite", "error")))
}

// TestReconcileDuration verifies that the ReconcileDuration histogram records
// observations per controller label.
func (s *MetricsTestSuite) TestReconcileDuration() {
	before := testutil.CollectAndCount(metrics.ReconcileDuration)
	metrics.ReconcileDuration.WithLabelValues("store").Observe(0.05)
	s.Assert().Greater(testutil.CollectAndCount(metrics.ReconcileDuration), before)
}

// TestFGAOperations verifies that the FGAOperations counter increments
// correctly for each operation/result label combination.
func (s *MetricsTestSuite) TestFGAOperations() {
	before := testutil.ToFloat64(metrics.FGAOperations.WithLabelValues("apply", "success"))
	metrics.FGAOperations.WithLabelValues("apply", "success").Inc()
	s.Require().Equal(before+1, testutil.ToFloat64(metrics.FGAOperations.WithLabelValues("apply", "success")))

	before = testutil.ToFloat64(metrics.FGAOperations.WithLabelValues("delete", "error"))
	metrics.FGAOperations.WithLabelValues("delete", "error").Inc()
	s.Require().Equal(before+1, testutil.ToFloat64(metrics.FGAOperations.WithLabelValues("delete", "error")))

	before = testutil.ToFloat64(metrics.FGAOperations.WithLabelValues("list", "success"))
	metrics.FGAOperations.WithLabelValues("list", "success").Inc()
	s.Require().Equal(before+1, testutil.ToFloat64(metrics.FGAOperations.WithLabelValues("list", "success")))
}
