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

package crypto_test

import (
	"bytes"
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/backup-operator/pkg/crypto"
)

var key = []byte("0123456789abcdef0123456789abcdef")

func TestRoundTrip(t *testing.T) {
	for _, size := range []int{0, 1, 64*1024 - 1, 64 * 1024, 64*1024 + 1, 3*64*1024 + 17} {
		plain := make([]byte, size)
		_, _ = rand.Read(plain)
		enc, err := crypto.Encrypt(key, plain)
		require.NoError(t, err)
		assert.Equal(t, crypto.EncryptedSize(int64(size)), int64(len(enc)), "size %d", size)

		got, err := crypto.Decrypt(key, enc)
		require.NoError(t, err, "size %d", size)
		assert.True(t, bytes.Equal(plain, got), "size %d", size)
	}
}

func TestDecryptFails(t *testing.T) {
	plain := make([]byte, 2*64*1024+5)
	enc, err := crypto.Encrypt(key, plain)
	require.NoError(t, err)

	_, err = crypto.Decrypt([]byte("ffffffffffffffffffffffffffffffff"), enc)
	assert.ErrorIs(t, err, crypto.ErrDecrypt)

	changed := bytes.Clone(enc)
	changed[len(changed)-1] ^= 1
	_, err = crypto.Decrypt(key, changed)
	assert.ErrorContains(t, err, "wrong key or broken data")

	// cut after the first chunk: the last chunk is missing
	_, err = crypto.Decrypt(key, enc[:7+64*1024+16])
	assert.ErrorIs(t, err, crypto.ErrDecrypt)

	_, err = crypto.Decrypt(key, append(bytes.Clone(enc), 'x'))
	assert.ErrorIs(t, err, crypto.ErrDecrypt)

	_, err = crypto.Decrypt(key, []byte("x"))
	assert.ErrorIs(t, err, crypto.ErrDecrypt)

	_, err = crypto.Encrypt([]byte("short"), []byte("secret"))
	assert.ErrorContains(t, err, "must be 32 bytes")
}
