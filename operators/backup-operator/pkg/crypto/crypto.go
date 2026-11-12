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

// Package crypto encrypts backup data as a stream of AES-256-GCM chunks of 64 KiB. The nonce has
// the chunk counter and a last-chunk flag, so chunks can not be reordered or cut off.
package crypto

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	// KeySize is the size of the encryption key in bytes.
	KeySize = 32

	chunkSize  = 64 * 1024
	prefixSize = 7
	tagSize    = 16
)

// ErrDecrypt is returned when the key is wrong or the data was changed.
var ErrDecrypt = errors.New("decrypting: wrong key or broken data")

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("encryption key must be %d bytes, got %d", KeySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func nonce(prefix []byte, counter uint32, last bool) []byte {
	n := make([]byte, 0, prefixSize+5)
	n = append(n, prefix...)
	n = binary.BigEndian.AppendUint32(n, counter)
	if last {
		return append(n, 1)
	}
	return append(n, 0)
}

// EncryptedSize is the size of n bytes after encryption.
func EncryptedSize(n int64) int64 {
	chunks := max((n+chunkSize-1)/chunkSize, 1)
	return prefixSize + n + chunks*tagSize
}

type writer struct {
	gcm     cipher.AEAD
	w       io.Writer
	prefix  []byte
	counter uint32
	buf     []byte
	out     []byte
	closed  bool
}

// NewWriter returns a writer that encrypts to w. Close writes the last chunk, it must be called.
func NewWriter(key []byte, w io.Writer) (io.WriteCloser, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	prefix := make([]byte, prefixSize)
	if _, err := rand.Read(prefix); err != nil {
		return nil, err
	}
	if _, err := w.Write(prefix); err != nil {
		return nil, err
	}
	return &writer{gcm: gcm, w: w, prefix: prefix, buf: make([]byte, 0, chunkSize), out: make([]byte, 0, chunkSize+tagSize)}, nil
}

func (w *writer) Write(p []byte) (int, error) {
	if w.closed {
		return 0, errors.New("write after close")
	}
	n := 0
	for len(p) > 0 {
		// a full chunk is sealed only when more data comes, the last chunk is sealed by Close
		if len(w.buf) == chunkSize {
			if err := w.seal(false); err != nil {
				return n, err
			}
		}
		c := copy(w.buf[len(w.buf):chunkSize], p)
		w.buf = w.buf[:len(w.buf)+c]
		p = p[c:]
		n += c
	}
	return n, nil
}

func (w *writer) seal(last bool) error {
	if w.counter == ^uint32(0) {
		return errors.New("data is too large to encrypt")
	}
	w.out = w.gcm.Seal(w.out[:0], nonce(w.prefix, w.counter, last), w.buf, nil)
	w.counter++
	w.buf = w.buf[:0]
	_, err := w.w.Write(w.out)
	return err
}

func (w *writer) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	return w.seal(true)
}

type reader struct {
	gcm     cipher.AEAD
	r       io.Reader
	prefix  []byte
	counter uint32
	enc     []byte
	plain   []byte
	off     int
	done    bool
}

// NewReader returns a reader that decrypts r. It returns ErrDecrypt when the key is wrong or the
// data was changed or cut off.
func NewReader(key []byte, r io.Reader) (io.Reader, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	prefix := make([]byte, prefixSize)
	if _, err := io.ReadFull(r, prefix); err != nil {
		return nil, ErrDecrypt
	}
	return &reader{gcm: gcm, r: r, prefix: prefix, enc: make([]byte, chunkSize+tagSize)}, nil
}

func (r *reader) Read(p []byte) (int, error) {
	for r.off == len(r.plain) {
		if r.done {
			return 0, io.EOF
		}
		if err := r.next(); err != nil {
			return 0, err
		}
	}
	n := copy(p, r.plain[r.off:])
	r.off += n
	return n, nil
}

func (r *reader) next() error {
	n, err := io.ReadFull(r.r, r.enc)
	switch {
	case errors.Is(err, io.ErrUnexpectedEOF):
		// a short chunk is the last one
		return r.open(r.enc[:n], true)
	case errors.Is(err, io.EOF):
		return ErrDecrypt
	case err != nil:
		return err
	}
	// a full chunk can be the last one too
	if err := r.open(r.enc, false); err == nil {
		return nil
	}
	if err := r.open(r.enc, true); err != nil {
		return err
	}
	var extra [1]byte
	if m, _ := io.ReadFull(r.r, extra[:]); m > 0 {
		return ErrDecrypt
	}
	return nil
}

func (r *reader) open(chunk []byte, last bool) error {
	plain, err := r.gcm.Open(r.plain[:0], nonce(r.prefix, r.counter, last), chunk, nil)
	if err != nil {
		return ErrDecrypt
	}
	r.plain, r.off, r.done = plain, 0, last
	r.counter++
	return nil
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
