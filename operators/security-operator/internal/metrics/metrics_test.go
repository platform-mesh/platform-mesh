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

package metrics_test

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/suite"

	"go.platform-mesh.io/security-operator/internal/metrics"
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
