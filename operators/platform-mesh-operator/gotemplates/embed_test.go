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

package gotemplates

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Guard that the embedded tree matches the on-disk one: every template on disk
// is embedded and nothing but templates ends up in the binary.
func TestResourcesEmbedEveryManifest(t *testing.T) {
	onDisk := map[string]struct{}{}
	err := filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".yaml") {
			onDisk[filepath.ToSlash(p)] = struct{}{}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	embedded := map[string]struct{}{}
	err = fs.WalkDir(Resources, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			embedded[p] = struct{}{}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	for p := range onDisk {
		if _, ok := embedded[p]; !ok {
			t.Errorf("%s exists on disk but is not embedded; extend the //go:embed pattern in embed.go", p)
		}
	}
	for p := range embedded {
		if _, ok := onDisk[p]; !ok {
			t.Errorf("%s is embedded but not a .yaml template on disk", p)
		}
	}
	if len(onDisk) == 0 {
		t.Fatal("no manifests found on disk")
	}
	if _, err := os.Stat("embed.go"); err != nil {
		t.Fatal(err)
	}
}
