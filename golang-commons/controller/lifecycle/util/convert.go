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

package util

import (
	"fmt"
	"reflect"

	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/golang-commons/sentry"
)

func ToInterface[T any](instance any, log *logger.Logger) (T, error) {
	var zero T
	obj, ok := instance.(T)
	if ok {
		return obj, nil
	}
	err := fmt.Errorf("failed to cast instance of type %T to %v", instance, reflect.TypeOf(zero))
	log.Error().Err(err).Msg("Failed to cast instance to target interface")
	sentry.CaptureError(err, nil)
	return zero, err
}

func MustToInterface[T any](instance any, log *logger.Logger) T {
	obj, err := ToInterface[T](instance, log)
	if err == nil {
		return obj
	}
	log.Panic().Err(err).Msg("Failed to cast instance to target interface")
	panic(err)
}
