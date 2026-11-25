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

package openfga

import (
	"context"
	"testing"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
)

// The store has to be there after a restart: kcp keeps the ids.
func TestStartKeepsStores(t *testing.T) {
	dir := t.TempDir()
	start := func() (*Server, context.CancelFunc) {
		ctx, cancel := context.WithCancel(t.Context())
		s, err := Start(ctx, Options{Port: 0, Dir: dir})
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		return s, cancel
	}

	s, stop := start()
	created, err := s.Client.CreateStore(t.Context(), &openfgav1.CreateStoreRequest{Name: "orgs"})
	if err != nil {
		t.Fatalf("CreateStore: %v", err)
	}
	stop()

	s, stop = start()
	defer stop()
	got, err := s.Client.GetStore(t.Context(), &openfgav1.GetStoreRequest{StoreId: created.GetId()})
	if err != nil {
		t.Fatalf("GetStore after restart: %v", err)
	}
	if got.GetName() != "orgs" {
		t.Fatalf("store name = %q, want orgs", got.GetName())
	}
}
