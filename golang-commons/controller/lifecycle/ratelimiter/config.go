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

package ratelimiter

import (
	"fmt"
	"time"
)

type Config struct {
	StaticRequeueDelay        time.Duration
	StaticWindow              time.Duration
	ExponentialInitialBackoff time.Duration
	ExponentialMaxBackoff     time.Duration
}

var defaultConfig = Config{
	StaticRequeueDelay:        2 * time.Second,
	StaticWindow:              60 * time.Second,
	ExponentialInitialBackoff: 2 * time.Second,
	ExponentialMaxBackoff:     1000 * time.Second,
}

func (c Config) validate() error {
	if c.StaticRequeueDelay < 0 {
		return fmt.Errorf("the static requeue delay shouldn't be negative")
	}
	if c.ExponentialInitialBackoff < 0 {
		return fmt.Errorf("the initial exponential backoff shouldn't be negative")
	}
	if c.StaticRequeueDelay > c.ExponentialInitialBackoff {
		return fmt.Errorf("the initial exponential backoff should be equal to or greater than the static requeue delay")
	}
	if c.StaticWindow < c.StaticRequeueDelay {
		return fmt.Errorf("the static window duration should be equal to or greater than the static requeue delay")
	}
	return nil
}

type Option func(*Config)

func WithStaticWindow(d time.Duration) Option {
	return func(c *Config) {
		c.StaticWindow = d
	}
}

func WithRequeueDelay(d time.Duration) Option {
	return func(c *Config) {
		c.StaticRequeueDelay = d
	}
}

func WithExponentialInitialBackoff(d time.Duration) Option {
	return func(c *Config) {
		c.ExponentialInitialBackoff = d
	}
}

func WithExponentialMaxBackoff(d time.Duration) Option {
	return func(c *Config) {
		c.ExponentialMaxBackoff = d
	}
}

func NewConfig(options ...Option) Config {
	cfg := defaultConfig

	for _, option := range options {
		option(&cfg)
	}

	return cfg
}
