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
	"time"

	"github.com/jellydator/ttlcache/v3"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"

	"go.platform-mesh.io/golang-commons/fga"
)

var _ fga.OpenFGAClientServicer = (*OpenFGAClient)(nil)

type OpenFGAClient struct {
	client openfgav1.OpenFGAServiceClient
	cache  *ttlcache.Cache[string, string]
}

func NewOpenFGAClient(openFGAServiceClient openfgav1.OpenFGAServiceClient) (*OpenFGAClient, error) {
	cache := ttlcache.New[string, string](
		ttlcache.WithTTL[string, string](5 * time.Minute),
	)

	go cache.Start()

	return &OpenFGAClient{
		client: openFGAServiceClient,
		cache:  cache,
	}, nil
}
