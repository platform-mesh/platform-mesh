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
