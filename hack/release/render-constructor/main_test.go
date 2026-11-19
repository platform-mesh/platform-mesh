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

package main

import "testing"

func TestRender(t *testing.T) {
	values := map[string]string{"PM_VERSION": "26.11.0", "COMMIT": "abc"}

	got, err := render("version: ${PM_VERSION}\ncommit: ${COMMIT}\nagain: ${PM_VERSION}\nprice: $5\n", values)
	if err != nil {
		t.Fatal(err)
	}
	want := "version: 26.11.0\ncommit: abc\nagain: 26.11.0\nprice: $5\n"
	if got != want {
		t.Errorf("render() = %q, want %q", got, want)
	}

	if _, err := render("url: ${REPO_URL}\n", values); err == nil {
		t.Error("render() with a placeholder that has no value: expected an error")
	}
}
