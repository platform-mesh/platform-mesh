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

package cmd

import (
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

type TestCase struct {
	name      string
	args      []string
	want      string
	ExpectErr bool
}

func TestRootCmd_Run(t *testing.T) {

	cases := []TestCase{
		{
			name: "2lines,no-colors",
			args: []string{"jl", "--no-colors", "-n2", "--input", "../data/input.log"},
			want: `
caller: /workspace/cmd/common.go:45, level: info, message: Logging on log level: info, service: extension-manager-operator, time: 2025-02-07T13:03:14Z
caller: /workspace/cmd/operator.go:128, level: info, message: starting manager, service: extension-manager-operator, time: 2025-02-07T13:03:14Z
`,
		},
		{
			name: "3lines,-no-colors",
			args: []string{"jl", "--no-colors", "-n3", "--input", "../data/input.log"},
			want: `
caller: /workspace/cmd/common.go:45, level: info, message: Logging on log level: info, service: extension-manager-operator, time: 2025-02-07T13:03:14Z
caller: /workspace/cmd/operator.go:128, level: info, message: starting manager, service: extension-manager-operator, time: 2025-02-07T13:03:14Z
caller: /go/pkg/mod/sigs.k8s.io/controller-runtime@v0.19.4/pkg/internal/controller/controller.go:175, component: controller-runtime, controller: ContentConfigurationReconciler, controllerGroup: core.openmfp.io, controllerKind: ContentConfiguration, level: debug, message: Starting EventSource, service: extension-manager-operator, source: kind source: *v1alpha1.ContentConfiguration, time: 2025-02-07T13:03:17Z
`,
		},
		{
			name: "3lines,-no-colors,select",
			args: []string{"jl", "--no-colors", "-n3", "--input", "../data/input.log", "--select=level=debug"},
			want: `
caller: /go/pkg/mod/sigs.k8s.io/controller-runtime@v0.19.4/pkg/internal/controller/controller.go:175, component: controller-runtime, controller: ContentConfigurationReconciler, controllerGroup: core.openmfp.io, controllerKind: ContentConfiguration, level: debug, message: Starting EventSource, service: extension-manager-operator, source: kind source: *v1alpha1.ContentConfiguration, time: 2025-02-07T13:03:17Z
`,
		},
		{
			name: "2lines",
			args: []string{"jl", "-n2", "--input", "../data/input.log"},
			want: "\n\x1b[94mcaller\x1b[0m: \x1b[0m/workspace/cmd/common.go:45\x1b[0m, \x1b[94mlevel\x1b[0m: \x1b[0minfo\x1b[0m, \x1b[94mmessage\x1b[0m: \x1b[32mLogging on log level: info\x1b[0m, \x1b[94mservice\x1b[0m: \x1b[0mextension-manager-operator\x1b[0m, \x1b[94mtime\x1b[0m: \x1b[0m2025-02-07T13:03:14Z\x1b[0m\n\x1b[94mcaller\x1b[0m: \x1b[0m/workspace/cmd/operator.go:128\x1b[0m, \x1b[94mlevel\x1b[0m: \x1b[0minfo\x1b[0m, \x1b[94mmessage\x1b[0m: \x1b[32mstarting manager\x1b[0m, \x1b[94mservice\x1b[0m: \x1b[0mextension-manager-operator\x1b[0m, \x1b[94mtime\x1b[0m: \x1b[0m2025-02-07T13:03:14Z\x1b[0m\n",
		},
		{
			name: "2lines,no-color,focus",
			args: []string{"jl", "-n2", "-f message", "--no-colors", "--input", "../data/input.log"},
			want: `
message: Logging on log level: info
message: starting manager
`,
		},
		{
			name: "2lines,no-color,focus,raw",
			args: []string{"jl", "-n2", "-rf message", "--no-colors", "--input", "../data/input.log"},
			want: `
Logging on log level: info
starting manager
`,
		},
		{
			name: "2lines,no-color,skip",
			args: []string{"jl", "-n2", "-s level,caller,service", "--no-colors", "--input", "../data/input.log"},
			want: `
message: Logging on log level: info, time: 2025-02-07T13:03:14Z
message: starting manager, time: 2025-02-07T13:03:14Z
`,
		},
		{
			name:      "panic fie not found",
			args:      []string{"jl", "--no-colors", "-n2", "--input", "../data/some-file"},
			ExpectErr: true,
		},
		{
			name:      "print-no-json",
			args:      []string{"jl", "--no-colors", "-n4", "--show-no-json", "--input", "../data/input.log"},
			ExpectErr: false,
			want:      "\ncaller: /workspace/cmd/common.go:45, level: info, message: Logging on log level: info, service: extension-manager-operator, time: 2025-02-07T13:03:14Z\ncaller: /workspace/cmd/operator.go:128, level: info, message: starting manager, service: extension-manager-operator, time: 2025-02-07T13:03:14Z\nI0207 13:03:14.507644       1 leaderelection.go:257] attempting to acquire leader lease openmfp-system/eengiex4.openmfp.io...\nE0207 13:03:14.507832       1 leaderelection.go:436] error retrieving resource lock openmfp-system/eengiex4.openmfp.io: Get \"https://10.96.0.1:443/apis/coordination.k8s.io/v1/namespaces/openmfp-system/leases/eengiex4.openmfp.io?timeout=5s\": dial tcp 10.96.0.1:443: connect: connection refused\n",
		},
		{
			name:      "1line,json,no focus,do not skip first key",
			args:      []string{"jl", "-n1", "-r", "--no-colors", "--input", "../data/input2.log"},
			want:      "\nfalse\n",
			ExpectErr: false,
		},
		{
			name:      "1line,json,space-lines,no focus,do not skip first key",
			args:      []string{"jl", "-n1", "--space-lines", "true", "--input", "../data/input2.log"},
			want:      "\n\x1b[94mallowed\x1b[0m: \x1b[0mfalse\x1b[0m\n\n",
			ExpectErr: false,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// Redirect stdout to a pipe to capture output
			old := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			// Reset the root command
			rootCmd.ResetCommands()
			rootCmd.ResetFlags()
			initialiseFlags()

			os.Args = tt.args
			if !tt.ExpectErr {
				err := rootCmd.Execute()
				assert.NoErrorf(t, err, "expected no error but got %v", err)

				// Close the write end of the pipe so that the read end can return EOF
				_ = w.Close()
				out, _ := io.ReadAll(r)
				os.Stdout = old

				// Assert the output
				assert.Equal(t, tt.want, string(out))
			} else {
				assert.Panics(t, func() {
					_ = rootCmd.Execute()
				})
			}
		})
	}
}
