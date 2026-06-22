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

package keys

import "go.platform-mesh.io/golang-commons/jwt"

type ContextKey string

const (
	RequestIdCtxKey     = ContextKey("request-id")
	LoggerCtxKey        = ContextKey("logger")
	ConfigCtxKey        = ContextKey("config")
	SentryTagsCtxKey    = ContextKey("sentryTags")
	TechnicalUserCtxKey = ContextKey("technicalUser")
	SpiffeCtxKey        = ContextKey(jwt.SpiffeCtxKey)
	TenantIdCtxKey      = ContextKey(jwt.TenantIdCtxKey)
	AuthHeaderCtxKey    = ContextKey(jwt.AuthHeaderCtxKey)
	WebTokenCtxKey      = ContextKey(jwt.WebTokenCtxKey)
	UserIDCtxKey        = ContextKey("userId")
)
