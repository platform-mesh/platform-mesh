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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsBlockedHost(t *testing.T) {
	t.Parallel()

	assert.True(t, IsBlockedHost("localhost"))
	assert.True(t, IsBlockedHost("foo.internal"))
	assert.True(t, IsBlockedHost("10.0.0.1"))
	assert.True(t, IsBlockedHost("100.64.0.1"))
	assert.True(t, IsBlockedHost("0.0.0.0"))
	assert.True(t, IsBlockedHost("::ffff:127.0.0.1"))
	assert.True(t, IsBlockedHost("service.local"))
	assert.False(t, IsBlockedHost("idp.example.com"))
}

func TestValidateHTTPSURL(t *testing.T) {
	t.Parallel()

	assert.NoError(t, ValidateHTTPSURL("https://idp.example.com/path"))
	assert.Error(t, ValidateHTTPSURL("http://idp.example.com/path"))
	assert.Error(t, ValidateHTTPSURL("https://127.0.0.1/path"))
}
