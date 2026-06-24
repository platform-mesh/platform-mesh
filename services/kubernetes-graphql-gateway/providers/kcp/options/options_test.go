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

package options

import (
	"testing"

	"k8s.io/client-go/rest"
)

func TestApplyLogicalClusterToConfig(t *testing.T) {
	tests := []struct {
		name           string
		host           string
		logicalCluster string
		wantHost       string
		wantErr        bool
	}{
		{
			name:           "empty logical cluster returns cfg unchanged",
			host:           "https://kcp.example.com/clusters/root",
			logicalCluster: "",
			wantHost:       "https://kcp.example.com/clusters/root",
		},
		{
			name:           "rewrites host path to logical cluster",
			host:           "https://kcp.example.com/clusters/root",
			logicalCluster: "root:providers",
			wantHost:       "https://kcp.example.com/clusters/root:providers",
		},
		{
			name:           "rewrites host with no path",
			host:           "https://kcp.example.com",
			logicalCluster: "root:providers",
			wantHost:       "https://kcp.example.com/clusters/root:providers",
		},
		{
			name:           "invalid host url returns error",
			host:           "://bad",
			logicalCluster: "root:providers",
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &CompletedOptions{
				completedOptions: &completedOptions{
					ExtraOptions: ExtraOptions{
						APIExportEndpointSliceLogicalCluster: tt.logicalCluster,
					},
				},
			}

			cfg := &rest.Config{Host: tt.host}
			got, err := opts.ApplyLogicalClusterToConfig(cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got.Host != tt.wantHost {
				t.Errorf("Host = %q, want %q", got.Host, tt.wantHost)
			}

			if tt.logicalCluster != "" && got == cfg {
				t.Errorf("expected copy of rest.Config, got same pointer")
			}
			if tt.logicalCluster == "" && got != cfg {
				t.Errorf("expected unchanged rest.Config to be returned as-is")
			}
		})
	}
}
