//go:build integration

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

// Package account runs the account-operator in-process against a sharded kcp
// and checks how it reconciles Accounts into Workspaces and AccountInfos.
package account

import (
	"os"
	"testing"

	"go.platform-mesh.io/tests/framework/kcp"
)

func TestMain(m *testing.M) {
	os.Exit(kcp.Main(m))
}
