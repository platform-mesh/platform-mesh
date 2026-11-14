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

package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"time"
)

// ErrNotFound is returned when an object does not exist.
var ErrNotFound = errors.New("object not found")

// Object is one object in a bucket.
type Object struct {
	Key          string
	Size         int64
	LastModified time.Time
}

// Store is one bucket in an S3 compatible object store.
type Store interface {
	// Put writes r to key. size is -1 when it is not known.
	Put(ctx context.Context, key string, r io.Reader, size int64) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	List(ctx context.Context, prefix string) ([]Object, error)
	Delete(ctx context.Context, key string) error
}

// Copy streams an object from one store to another and returns its sha256.
// The stores can use different credentials.
func Copy(ctx context.Context, src Store, srcKey string, dst Store, dstKey string, size int64) (string, error) {
	r, err := src.Get(ctx, srcKey)
	if err != nil {
		return "", err
	}
	defer r.Close() //nolint:errcheck
	h := sha256.New()
	if err := dst.Put(ctx, dstKey, io.TeeReader(r, h), size); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// PutBytes writes data to key.
func PutBytes(ctx context.Context, s Store, key string, data []byte) error {
	return s.Put(ctx, key, bytesReader(data), int64(len(data)))
}

// GetBytes reads the whole object.
func GetBytes(ctx context.Context, s Store, key string) ([]byte, error) {
	r, err := s.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer r.Close() //nolint:errcheck
	return io.ReadAll(r)
}
