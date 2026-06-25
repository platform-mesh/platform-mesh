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

package client

import (
	"context"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
)

func (c *OpenFGAClient) Writes(ctx context.Context, writes []*openfgav1.TupleKey, deletes []*openfgav1.TupleKeyWithoutCondition, tenantId string) (bool, error) {
	storeId, err := c.StoreId(ctx, tenantId)
	if err != nil {
		return false, err
	}

	modelId, err := c.ModelId(ctx, tenantId)
	if err != nil {
		return false, err
	}
	req := &openfgav1.WriteRequest{
		StoreId:              storeId,
		AuthorizationModelId: modelId,
	}

	if writes != nil {
		req.Writes = &openfgav1.WriteRequestWrites{TupleKeys: writes}
	}
	if deletes != nil {
		req.Deletes = &openfgav1.WriteRequestDeletes{TupleKeys: deletes}
	}

	_, err = c.client.Write(ctx, req)
	if err != nil {
		return false, err
	}
	return true, nil
}

func (c *OpenFGAClient) Write(ctx context.Context, object string, relation string, user string, tenantId string) (bool, error) {
	return c.Writes(ctx, []*openfgav1.TupleKey{{Object: object, Relation: relation, User: user}}, nil, tenantId)
}

func (c *OpenFGAClient) Delete(ctx context.Context, object string, relation string, user string, tenantId string) (bool, error) {
	return c.Writes(ctx, nil, []*openfgav1.TupleKeyWithoutCondition{{Object: object, Relation: relation, User: user}}, tenantId)
}

func (c *OpenFGAClient) WriteIfNeeded(ctx context.Context, tuples []*openfgav1.TupleKeyWithoutCondition, tenantId string) error {
	tuplesToWrite := []*openfgav1.TupleKey{}
	for _, tuple := range tuples {
		exists, err := c.Exists(ctx, tuple, tenantId)
		if err != nil {
			return err
		}
		if !exists {
			tuplesToWrite = append(tuplesToWrite, &openfgav1.TupleKey{
				User:     tuple.User,
				Relation: tuple.Relation,
				Object:   tuple.Object,
			})
		}
	}

	if len(tuplesToWrite) > 0 {
		_, err := c.Writes(ctx, tuplesToWrite, nil, tenantId)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *OpenFGAClient) DeleteIfNeeded(ctx context.Context, tuples []*openfgav1.TupleKeyWithoutCondition, tenantId string) error {
	tuplesToDelete := []*openfgav1.TupleKeyWithoutCondition{}
	for _, tuple := range tuples {
		exists, err := c.Exists(ctx, tuple, tenantId)
		if err != nil {
			return err
		}
		if exists {
			tuplesToDelete = append(tuplesToDelete, tuple)
		}
	}

	if len(tuplesToDelete) > 0 {
		_, err := c.Writes(ctx, nil, tuplesToDelete, tenantId)
		if err != nil {
			return err
		}
	}
	return nil
}
