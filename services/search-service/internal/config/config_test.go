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

package config

import (
	"testing"

	"github.com/spf13/pflag"
)

func TestUserClaimConfiguration(t *testing.T) {
	cfg := NewServiceConfig()
	if cfg.UserClaim != "sub" {
		t.Fatalf("expected default user claim sub, got %q", cfg.UserClaim)
	}

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	cfg.AddFlags(flags)
	if err := flags.Parse([]string{"--user-claim=preferred_username"}); err != nil {
		t.Fatalf("parse user claim flag: %v", err)
	}
	if cfg.UserClaim != "preferred_username" {
		t.Fatalf("expected configured user claim preferred_username, got %q", cfg.UserClaim)
	}
}

func TestNewServiceConfigOpenFGADefaults(t *testing.T) {
	cfg := NewServiceConfig()

	if cfg.BatchSize != 50 {
		t.Errorf("BatchSize = %d, want 50", cfg.BatchSize)
	}
	if cfg.OpenFGA.ObjectType != "core_platform-mesh_io_account" {
		t.Errorf("OpenFGA.ObjectType = %q, want %q", cfg.OpenFGA.ObjectType, "core_platform-mesh_io_account")
	}
	if cfg.OpenFGA.DefaultRole != "member" {
		t.Errorf("OpenFGA.DefaultRole = %q, want %q", cfg.OpenFGA.DefaultRole, "member")
	}
}
