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

package context

import (
	"context"
	"errors"
)

type key string

const requestContextKey key = "request-context"

type RequestContext struct {
	Organization string
	User         string
	IDMTenant    string
}

func WithRequestContext(ctx context.Context, rc RequestContext) context.Context {
	return context.WithValue(ctx, requestContextKey, rc)
}

func GetRequestContext(ctx context.Context) (RequestContext, error) {
	v := ctx.Value(requestContextKey)
	if v == nil {
		return RequestContext{}, errors.New("request context missing")
	}
	rc, ok := v.(RequestContext)
	if !ok {
		return RequestContext{}, errors.New("request context has invalid type")
	}
	if rc.Organization == "" || rc.User == "" {
		return RequestContext{}, errors.New("request context is incomplete")
	}
	return rc, nil
}
