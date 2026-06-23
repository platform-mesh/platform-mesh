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

	"go.platform-mesh.io/search-operator/internal/metrics"
)

type MetricsTestSuite struct {
	suite.Suite
}

func TestMetricsTestSuite(t *testing.T) {
	suite.Run(t, new(MetricsTestSuite))
}

// TestSubroutineTotal verifies that the SubroutineTotal counter increments
// correctly for each subroutine/result label combination.
func (s *MetricsTestSuite) TestSubroutineTotal() {
	before := testutil.ToFloat64(metrics.SubroutineTotal.WithLabelValues("IndexLifecycle", "success"))
	metrics.SubroutineTotal.WithLabelValues("IndexLifecycle", "success").Inc()
	s.Require().Equal(before+1, testutil.ToFloat64(metrics.SubroutineTotal.WithLabelValues("IndexLifecycle", "success")))

	before = testutil.ToFloat64(metrics.SubroutineTotal.WithLabelValues("IndexableResourceWatcher", "error"))
	metrics.SubroutineTotal.WithLabelValues("IndexableResourceWatcher", "error").Inc()
	s.Require().Equal(before+1, testutil.ToFloat64(metrics.SubroutineTotal.WithLabelValues("IndexableResourceWatcher", "error")))
}

// TestSubroutineDuration verifies that the SubroutineDuration histogram records
// observations per subroutine label.
func (s *MetricsTestSuite) TestSubroutineDuration() {
	before := testutil.CollectAndCount(metrics.SubroutineDuration)
	metrics.SubroutineDuration.WithLabelValues("APIBindingWatcher").Observe(0.1)
	s.Assert().Greater(testutil.CollectAndCount(metrics.SubroutineDuration), before)
}

// TestOpenSearchOperationsTotal verifies that the OpenSearchOperationsTotal counter
// increments correctly for each operation/result label combination.
func (s *MetricsTestSuite) TestOpenSearchOperationsTotal() {
	before := testutil.ToFloat64(metrics.OpenSearchOperationsTotal.WithLabelValues("index_document", "success"))
	metrics.OpenSearchOperationsTotal.WithLabelValues("index_document", "success").Inc()
	s.Require().Equal(before+1, testutil.ToFloat64(metrics.OpenSearchOperationsTotal.WithLabelValues("index_document", "success")))

	before = testutil.ToFloat64(metrics.OpenSearchOperationsTotal.WithLabelValues("create_index", "error"))
	metrics.OpenSearchOperationsTotal.WithLabelValues("create_index", "error").Inc()
	s.Require().Equal(before+1, testutil.ToFloat64(metrics.OpenSearchOperationsTotal.WithLabelValues("create_index", "error")))
}

// TestOpenSearchOperationDuration verifies that the OpenSearchOperationDuration histogram
// records observations per operation label.
func (s *MetricsTestSuite) TestOpenSearchOperationDuration() {
	before := testutil.CollectAndCount(metrics.OpenSearchOperationDuration)
	metrics.OpenSearchOperationDuration.WithLabelValues("index_document").Observe(0.02)
	s.Assert().Greater(testutil.CollectAndCount(metrics.OpenSearchOperationDuration), before)
}
