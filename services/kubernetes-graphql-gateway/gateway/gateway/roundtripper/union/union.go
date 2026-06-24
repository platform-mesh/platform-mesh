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

package union

import (
	"errors"
	"net/http"
)

// Handler processes HTTP requests and indicates whether it handled the request.
type Handler interface {
	RoundTrip(req *http.Request) (resp *http.Response, err error, handled bool)
}

type roundTripperUnion struct {
	handlers []Handler
}

func (u *roundTripperUnion) RoundTrip(req *http.Request) (*http.Response, error) {
	for _, h := range u.handlers {
		resp, err, handled := h.RoundTrip(req)
		if handled {
			return resp, err
		}
	}
	return nil, errors.New("no handler processed the request")
}

var _ http.RoundTripper = &roundTripperUnion{}

// New creates a union roundtripper from the given handlers.
// Handlers are tried in order until one handles the request.
func New(handlers ...Handler) http.RoundTripper {
	if len(handlers) == 1 {
		return &singleHandler{h: handlers[0]}
	}
	return &roundTripperUnion{handlers: handlers}
}

type singleHandler struct {
	h Handler
}

func (s *singleHandler) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err, _ := s.h.RoundTrip(req)
	return resp, err
}
