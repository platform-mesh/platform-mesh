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

package server

import (
	"html"
	"net/http"

	"go.platform-mesh.io/golang-commons/logger"
)

type requestLogger struct {
	log *logger.Logger
}

func (rl *requestLogger) Handler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rl.log.Debug().Msgf("Request from %s %s %s", r.RemoteAddr, r.Method, html.EscapeString(r.URL.Path))
		h.ServeHTTP(w, r)
	})
}
