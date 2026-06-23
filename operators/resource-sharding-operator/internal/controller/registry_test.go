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

//nolint:goconst
package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// ---------------------------------------------------------------------------
// NewDynamicControllerRegistry
// ---------------------------------------------------------------------------

func TestRegistry_NewRegistry_IsEmpty(t *testing.T) {
	r := NewDynamicControllerRegistry()
	require.NotNil(t, r)

	_, ok := r.Get(types.UID("any"))
	assert.False(t, ok, "new registry should have no entries")
}

// ---------------------------------------------------------------------------
// Register / Get
// ---------------------------------------------------------------------------

func TestRegistry_Register_ThenGet(t *testing.T) {
	r := NewDynamicControllerRegistry()
	uid := types.UID("uid-1")
	rc := &RunningController{
		Cancel:   func() {},
		GVR:      schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
		LabelKey: "shard.io/shard",
		Assigner: NewShardAssigner([]string{"shard-a"}),
	}

	r.Register(uid, rc)

	got, ok := r.Get(uid)
	require.True(t, ok)
	assert.Equal(t, rc, got)
}

func TestRegistry_Get_MissingUID_ReturnsFalse(t *testing.T) {
	r := NewDynamicControllerRegistry()

	_, ok := r.Get(types.UID("nonexistent"))
	assert.False(t, ok)
}

// ---------------------------------------------------------------------------
// Deregister
// ---------------------------------------------------------------------------

func TestRegistry_Deregister_CallsCancelAndRemoves(t *testing.T) {
	r := NewDynamicControllerRegistry()
	uid := types.UID("uid-2")
	cancelled := false
	rc := &RunningController{
		Cancel: func() { cancelled = true },
		GVR:    schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
	}

	r.Register(uid, rc)
	r.Deregister(uid)

	assert.True(t, cancelled, "Cancel should have been called on deregister")
	_, ok := r.Get(uid)
	assert.False(t, ok, "entry should be removed after Deregister")
}

func TestRegistry_Deregister_NoopOnMissingUID(t *testing.T) {
	r := NewDynamicControllerRegistry()
	// Should not panic
	require.NotPanics(t, func() {
		r.Deregister(types.UID("nonexistent"))
	})
}

// ---------------------------------------------------------------------------
// HasGVR
// ---------------------------------------------------------------------------

func TestRegistry_HasGVR_ReturnsTrueWhenOtherEntryMatchesGVR(t *testing.T) {
	r := NewDynamicControllerRegistry()
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

	uid1 := types.UID("uid-1")
	uid2 := types.UID("uid-2")

	r.Register(uid1, &RunningController{Cancel: func() {}, GVR: gvr})
	r.Register(uid2, &RunningController{Cancel: func() {}, GVR: gvr})

	// uid1 checks: uid2 has the same GVR → true
	assert.True(t, r.HasGVR(gvr, uid1))
}

func TestRegistry_HasGVR_ReturnsFalseWhenOnlySelfMatchesGVR(t *testing.T) {
	r := NewDynamicControllerRegistry()
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	uid := types.UID("uid-self")

	r.Register(uid, &RunningController{Cancel: func() {}, GVR: gvr})

	// Excluding the only entry that matches → false
	assert.False(t, r.HasGVR(gvr, uid))
}

func TestRegistry_HasGVR_ReturnsFalseWhenNoEntries(t *testing.T) {
	r := NewDynamicControllerRegistry()
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

	assert.False(t, r.HasGVR(gvr, types.UID("any")))
}

func TestRegistry_HasGVR_ReturnsFalseWhenDifferentGVR(t *testing.T) {
	r := NewDynamicControllerRegistry()
	gvr1 := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	gvr2 := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}

	uid1 := types.UID("uid-1")
	uid2 := types.UID("uid-2")

	r.Register(uid1, &RunningController{Cancel: func() {}, GVR: gvr1})
	r.Register(uid2, &RunningController{Cancel: func() {}, GVR: gvr2})

	// Check whether gvr2 is held by any entry other than uid2 — it's not (only uid2 has gvr2, and uid2 is excluded).
	assert.False(t, r.HasGVR(gvr2, uid2))
}

// ---------------------------------------------------------------------------
// FindByGVR
// ---------------------------------------------------------------------------

func TestRegistry_FindByGVR_ReturnsMatchingController(t *testing.T) {
	r := NewDynamicControllerRegistry()
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	uid := types.UID("uid-find")
	rc := &RunningController{Cancel: func() {}, GVR: gvr}

	r.Register(uid, rc)

	found := r.FindByGVR("apps", "v1", "deployments")
	require.NotNil(t, found)
	assert.Equal(t, rc, found)
}

func TestRegistry_FindByGVR_ReturnsNilWhenNotFound(t *testing.T) {
	r := NewDynamicControllerRegistry()

	found := r.FindByGVR("apps", "v1", "nonexistent")
	assert.Nil(t, found)
}

func TestRegistry_FindByGVR_PartialMatchReturnNil(t *testing.T) {
	r := NewDynamicControllerRegistry()
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	r.Register(types.UID("uid-1"), &RunningController{Cancel: func() {}, GVR: gvr})

	// Only group matches
	assert.Nil(t, r.FindByGVR("apps", "v1", "replicasets"))
	// Only version matches
	assert.Nil(t, r.FindByGVR("batch", "v1", "deployments"))
}

// ---------------------------------------------------------------------------
// Concurrency — no data races (run with -race)
// ---------------------------------------------------------------------------

func TestRegistry_ConcurrentAccess_NoDataRace(t *testing.T) {
	r := NewDynamicControllerRegistry()
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

	uid := types.UID("uid-race")
	r.Register(uid, &RunningController{Cancel: func() {}, GVR: gvr})

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 100 {
			_ = r.HasGVR(gvr, types.UID("other"))
			_ = r.FindByGVR("apps", "v1", "deployments")
			_, _ = r.Get(uid)
		}
	}()

	for range 100 {
		uid2 := types.UID("uid-tmp")
		r.Register(uid2, &RunningController{Cancel: func() {}, GVR: gvr})
		r.Deregister(uid2)
	}

	<-done
}
