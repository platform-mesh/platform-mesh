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
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-http-utils/headers"
	"github.com/rs/cors"
	"go.platform-mesh.io/golang-commons/logger"

	"go.platform-mesh.io/extension-manager-operator/pkg/validation"
)

func CreateRouter(
	isLocal bool,
	log *logger.Logger,
	validator validation.ExtensionConfiguration,
) *chi.Mux {
	router := chi.NewRouter()

	if isLocal {
		rl := requestLogger{
			log: log,
		}

		router.Use(cors.New(cors.Options{
			AllowedOrigins:   []string{"*"},
			AllowCredentials: true,
			AllowedHeaders:   []string{headers.Accept, headers.Authorization, headers.ContentType, headers.XCSRFToken},
			Debug:            false,
			AllowedMethods:   []string{http.MethodPost, http.MethodGet},
		}).Handler)
		router.Use(rl.Handler)
	}

	vh := NewHttpValidateHandler(log, validator)

	router.MethodFunc(http.MethodPost, "/validate", vh.HandlerValidate)
	router.MethodFunc(http.MethodGet, "/healthz", vh.HandlerHealthz)
	router.MethodFunc(http.MethodGet, "/readyz", vh.HandlerHealthz)

	return router
}
