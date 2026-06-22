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

package config_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"go.platform-mesh.io/golang-commons/config"
)

func TestSetConfigInContext(t *testing.T) {
	ctx := context.Background()
	configStr := "test"
	ctx = config.SetConfigInContext(ctx, configStr)

	retrievedConfig := config.LoadConfigFromContext(ctx)
	assert.Equal(t, configStr, retrievedConfig)
}

func TestNewDefaultConfig(t *testing.T) {
	cfg := config.NewDefaultConfig()

	assert.NotNil(t, cfg)
	assert.Equal(t, 10, cfg.MaxConcurrentReconciles)
	assert.Equal(t, "local", cfg.Region)
	assert.Equal(t, "info", cfg.Log.Level)
	assert.Equal(t, ":9090", cfg.Metrics.BindAddress)
	assert.Equal(t, ":8090", cfg.HealthProbeBindAddress)
	assert.Equal(t, time.Minute, cfg.ShutdownTimeout)
	assert.True(t, cfg.EnableHTTP2)
}
