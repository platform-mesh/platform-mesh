package metrics_test

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/suite"

	"go.platform-mesh.io/provider-operator/internal/metrics"
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
	before := testutil.ToFloat64(metrics.ReconcileTotal.WithLabelValues("PlatformMeshReconciler", "success"))
	metrics.ReconcileTotal.WithLabelValues("PlatformMeshReconciler", "success").Inc()
	s.Require().Equal(before+1, testutil.ToFloat64(metrics.ReconcileTotal.WithLabelValues("PlatformMeshReconciler", "success")))

	before = testutil.ToFloat64(metrics.ReconcileTotal.WithLabelValues("ResourceReconciler", "error"))
	metrics.ReconcileTotal.WithLabelValues("ResourceReconciler", "error").Inc()
	s.Require().Equal(before+1, testutil.ToFloat64(metrics.ReconcileTotal.WithLabelValues("ResourceReconciler", "error")))
}

// TestSubroutineTotal verifies that the SubroutineTotal counter increments
// correctly for each subroutine/result label combination.
func (s *MetricsTestSuite) TestSubroutineTotal() {
	before := testutil.ToFloat64(metrics.SubroutineTotal.WithLabelValues("deployment", "success"))
	metrics.SubroutineTotal.WithLabelValues("deployment", "success").Inc()
	s.Require().Equal(before+1, testutil.ToFloat64(metrics.SubroutineTotal.WithLabelValues("deployment", "success")))

	before = testutil.ToFloat64(metrics.SubroutineTotal.WithLabelValues("kcpsetup", "error"))
	metrics.SubroutineTotal.WithLabelValues("kcpsetup", "error").Inc()
	s.Require().Equal(before+1, testutil.ToFloat64(metrics.SubroutineTotal.WithLabelValues("kcpsetup", "error")))
}

// TestSubroutineDuration verifies that the SubroutineDuration histogram records
// observations per subroutine label.
func (s *MetricsTestSuite) TestSubroutineDuration() {
	before := testutil.CollectAndCount(metrics.SubroutineDuration)
	metrics.SubroutineDuration.WithLabelValues("deployment").Observe(0.1)
	s.Assert().Greater(testutil.CollectAndCount(metrics.SubroutineDuration), before)
}
