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

package clientreg

import (
	"context"
	"net/http"
	"time"
)

type TokenProvider interface {
	TokenForRegistration(ctx context.Context) (string, error)
}

type clientOptions struct {
	httpClient    *http.Client
	tokenProvider TokenProvider
}

type Option func(*clientOptions)

func WithHTTPClient(c *http.Client) Option {
	return func(o *clientOptions) {
		o.httpClient = c
	}
}

func WithTokenProvider(p TokenProvider) Option {
	return func(o *clientOptions) {
		o.tokenProvider = p
	}
}

func defaultOptions() *clientOptions {
	return &clientOptions{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}
