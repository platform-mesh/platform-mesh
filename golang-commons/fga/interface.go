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

package fga

import (
	"context"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
)

type OpenFGAClientServicer interface {
	Check(ctx context.Context, object string, relation string, user string, tenantId string) (*openfgav1.CheckResponse, error)
	Read(ctx context.Context, object *string, relation *string, user *string, tenantId string) (*openfgav1.ReadResponse, error)
	Exists(ctx context.Context, tuple *openfgav1.TupleKeyWithoutCondition, tenantId string) (bool, error)
	Writes(ctx context.Context, writes []*openfgav1.TupleKey, deletes []*openfgav1.TupleKeyWithoutCondition, tenantId string) (bool, error)
	Write(ctx context.Context, object string, relation string, user string, tenantId string) (bool, error)
	WriteIfNeeded(ctx context.Context, tuples []*openfgav1.TupleKeyWithoutCondition, tenantId string) error
	DeleteIfNeeded(ctx context.Context, tuples []*openfgav1.TupleKeyWithoutCondition, tenantId string) error
	Delete(ctx context.Context, object string, relation string, user string, tenantId string) (bool, error)
	ModelId(ctx context.Context, tenantId string) (string, error)
	StoreId(ctx context.Context, tenantId string) (string, error)
}
