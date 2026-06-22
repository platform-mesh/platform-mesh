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
	"math/rand/v2"
	"time"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	"go.platform-mesh.io/golang-commons/controller/lifecycle/api"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/runtimeobject"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/util"
	"go.platform-mesh.io/golang-commons/logger"
)

const ReconcileRefreshLabel = "platform-mesh.io/refresh-reconcile"

type Spreader struct {
}

func NewSpreader() *Spreader {
	return &Spreader{}
}

type GenerateNextReconcileTimer interface {
	GenerateNextReconcileTime() time.Duration
}

const defaultMaxReconcileDuration = 24 * time.Hour

// getNextReconcileTime returns a random time between [maxReconcileTime]/2 and [maxReconcileTime] hours
func getNextReconcileTime(maxReconcileTime time.Duration) time.Duration {

	minTime := maxReconcileTime.Minutes() / 2

	jitter := rand.Int64N(int64(minTime))

	return time.Duration(jitter+int64(minTime)) * time.Minute
}

func (s *Spreader) OnNextReconcile(instance runtimeobject.RuntimeObject, log *logger.Logger) (ctrl.Result, error) {
	instanceStatusObj := util.MustToInterface[api.RuntimeObjectSpreadReconcileStatus](instance, log)
	requeueAfter := time.Until(instanceStatusObj.GetNextReconcileTime().UTC())
	log.Debug().Int64("minutes-till-next-execution", int64(requeueAfter.Minutes())).Msg("Completed reconciliation, no processing needed")
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// SetNextReconcileTime calculates and sets the next reconcile time for the instance
func (s *Spreader) SetNextReconcileTime(instanceStatusObj api.RuntimeObjectSpreadReconcileStatus, log *logger.Logger) {

	var border = defaultMaxReconcileDuration
	if in, ok := instanceStatusObj.(GenerateNextReconcileTimer); ok {
		border = in.GenerateNextReconcileTime()
	}

	nextReconcileTime := getNextReconcileTime(border)

	log.Debug().Int64("minutes-till-next-execution", int64(nextReconcileTime.Minutes())).Msg("Setting next reconcile time for the instance")
	instanceStatusObj.SetNextReconcileTime(v1.NewTime(time.Now().Add(nextReconcileTime)))
}

// UpdateObservedGeneration updates the observed generation of the instance struct
func (s *Spreader) UpdateObservedGeneration(instanceStatusObj api.RuntimeObjectSpreadReconcileStatus, log *logger.Logger) {
	log.Debug().Int64("observed-generation", instanceStatusObj.GetObservedGeneration()).Int64("generation", instanceStatusObj.GetGeneration()).Msg("Updating observed generation")
	instanceStatusObj.SetObservedGeneration(instanceStatusObj.GetGeneration())
}
func (s *Spreader) RemoveRefreshLabelIfExists(instance runtimeobject.RuntimeObject) bool {
	keyCount := len(instance.GetLabels())
	delete(instance.GetLabels(), ReconcileRefreshLabel)
	return keyCount != len(instance.GetLabels())
}

func (s *Spreader) ReconcileRequired(instance runtimeobject.RuntimeObject, log *logger.Logger) bool {

	instanceStatusObj := util.MustToInterface[api.RuntimeObjectSpreadReconcileStatus](instance, log)
	generationChanged := instance.GetGeneration() != instanceStatusObj.GetObservedGeneration()
	isAfterNextReconcileTime := v1.Now().UTC().After(instanceStatusObj.GetNextReconcileTime().UTC())
	_, refreshRequested := instance.GetLabels()[ReconcileRefreshLabel]

	return generationChanged || isAfterNextReconcileTime || refreshRequested
}
