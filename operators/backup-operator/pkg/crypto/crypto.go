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

// Package crypto encrypts backup data with DARE 2.0 of github.com/minio/sio. Each stream starts
// with a random nonce, the stream key is derived from the master key and this nonce.
package crypto

import (
	"bufio"
	"bytes"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"github.com/minio/sio"
)

const (
	// KeySize is the size of the encryption key in bytes.
	KeySize = 32

	nonceSize = 32
)

// ErrDecrypt is returned when the key is wrong or the data was changed.
var ErrDecrypt = errors.New("decrypting: wrong key or broken data")

func config(key, nonce []byte) (sio.Config, error) {
	if len(key) != KeySize {
		return sio.Config{}, fmt.Errorf("encryption key must be %d bytes, got %d", KeySize, len(key))
	}
	streamKey, err := hkdf.Key(sha256.New, key, nonce, "backup-operator", 32)
	if err != nil {
		return sio.Config{}, err
	}
	return sio.Config{Key: streamKey, MinVersion: sio.Version20, MaxVersion: sio.Version20}, nil
}

// EncryptedSize is the size of n bytes after encryption.
func EncryptedSize(n int64) int64 {
	size, err := sio.EncryptedSize(uint64(n))
	if err != nil {
		panic(err)
	}
	return nonceSize + int64(size)
}

// NewWriter returns a writer that encrypts to w. Close writes the last package, it must be called.
func NewWriter(key []byte, w io.Writer) (io.WriteCloser, error) {
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	cfg, err := config(key, nonce)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(nonce); err != nil {
		return nil, err
	}
	// sio closes its destination on Close, the archive writes more entries after this one
	return sio.EncryptWriter(struct{ io.Writer }{w}, cfg)
}

type reader struct{ r io.Reader }

func (r reader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	var sioErr sio.Error
	if errors.As(err, &sioErr) || errors.Is(err, io.ErrUnexpectedEOF) {
		return n, ErrDecrypt
	}
	return n, err
}

// NewReader returns a reader that decrypts r. It returns ErrDecrypt when the key is wrong or the
// data was changed or cut off.
func NewReader(key []byte, r io.Reader) (io.Reader, error) {
	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(r, nonce); err != nil {
		return nil, ErrDecrypt
	}
	cfg, err := config(key, nonce)
	if err != nil {
		return nil, err
	}
	// sio writes nothing for empty data
	br := bufio.NewReader(r)
	if _, err := br.Peek(1); errors.Is(err, io.EOF) {
		return bytes.NewReader(nil), nil
	}
	dr, err := sio.DecryptReader(br, cfg)
	if err != nil {
		return nil, err
	}
	return reader{dr}, nil
}

// Encrypt encrypts a small value in memory.
func Encrypt(key, plaintext []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := NewWriter(key, &buf)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(plaintext); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Decrypt reverses Encrypt.
func Decrypt(key, data []byte) ([]byte, error) {
	r, err := NewReader(key, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}
