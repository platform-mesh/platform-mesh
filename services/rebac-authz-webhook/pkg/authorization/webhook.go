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

package authorization

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-logr/logr"

	authorizationv1 "k8s.io/api/authorization/v1"
	authorizationv1beta1 "k8s.io/api/authorization/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
)

var authorizationScheme = runtime.NewScheme()
var authorizationCodecs = serializer.NewCodecFactory(authorizationScheme)

func init() {
	utilruntime.Must(authorizationv1.AddToScheme(authorizationScheme))
	utilruntime.Must(authorizationv1beta1.AddToScheme(authorizationScheme))
}

var _ http.Handler = &Webhook{}

// Handler can handle an SubjectAccessReview.
type Handler interface {
	// Handle yields a response to an SubjectAccessReview.
	//
	// The supplied context is extracted from the received http.Request, allowing wrapping
	// http.Handlers to inject values into and control cancelation of downstream request processing.
	Handle(context.Context, Request) Response
}

// HandlerFunc implements Handler interface using a single function.
type HandlerFunc func(context.Context, Request) Response

var _ Handler = HandlerFunc(nil)

// Handle processes the SubjectAccessReview by invoking the underlying function.
func (f HandlerFunc) Handle(ctx context.Context, req Request) Response {
	return f(ctx, req)
}

// Webhook represents each individual webhook.
type Webhook struct {
	// Handler actually processes an authorization request returning whether it was authorized or unauthorized.
	Handler Handler

	log logr.Logger
}

func New(log logr.Logger, handler Handler) *Webhook {
	return &Webhook{
		Handler: handler,
		log:     log.WithName("webhook"),
	}
}

// Request defines the input for an authorization handler.
type Request struct {
	authorizationv1.SubjectAccessReview
}

// Response is the output of an authorization handler.
// It contains a response indicating if a given
// operation is allowed.
type Response struct {
	authorizationv1.SubjectAccessReview
	Abort      bool          `json:"-"`
	RetryAfter time.Duration `json:"-"`
}

// ServeHTTP implements http.Handler.
func (wh *Webhook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if r.Body == nil || r.Body == http.NoBody {
		err := errors.New("request body is empty")
		wh.log.Error(err, "empty request body")
		wh.writeResponse(w, Errored(err))
		return
	}

	defer r.Body.Close() //nolint:errcheck

	if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
		err := fmt.Errorf("contentType=%s, expected application/json", contentType)
		wh.log.Error(err, "invalid content type")
		wh.writeResponse(w, Errored(err))
		return
	}

	// prevent unbound reads, 1MiB should be enough for a SAR
	maxReader := http.MaxBytesReader(w, r.Body, 1<<20)

	body, err := io.ReadAll(maxReader)
	if err != nil {
		wh.log.Error(err, "unable to read the body from the incoming request")
		wh.writeResponse(w, Errored(err))
		return
	}

	// Both v1 and v1beta1 SubjectAccessReview types are exactly the same, so the v1beta1 type can
	// be decoded into the v1 type. However the runtime codec's decoder guesses which type to
	// decode into by type name if an Object's TypeMeta isn't set. By setting TypeMeta of an
	// unregistered type to the v1 GVK, the decoder will coerce a v1beta1 SubjectAccessReview to authenticationv1.
	req := Request{}
	sar := unversionedSubjectAccessReview{}
	sar.SubjectAccessReview = &req.SubjectAccessReview
	sar.SetGroupVersionKind(authorizationv1.SchemeGroupVersion.WithKind("SubjectAccessReview"))

	_, _, err = authorizationCodecs.UniversalDecoder().Decode(body, nil, &sar)
	if err != nil {
		wh.log.Error(err, "unable to decode the request")
		wh.writeResponse(w, Errored(err))
		return
	}

	// TODO: think of log constructor
	wh.log.V(5).Info("received request")

	res := wh.Handler.Handle(ctx, req)
	res.UID = req.UID
	wh.writeResponse(w, res)
}

func (wh *Webhook) writeResponse(w http.ResponseWriter, resp Response) {
	if resp.RetryAfter != 0 {
		seconds := strconv.Itoa(int(resp.RetryAfter.Seconds()))
		w.Header().Add("Retry-After", seconds)
		w.WriteHeader(503)
		wh.log.V(5).Info("Wrote response", "retry_after", resp.RetryAfter)
		return
	}

	if err := json.NewEncoder(w).Encode(resp.SubjectAccessReview); err != nil {
		wh.log.Error(err, "unable to encode the response")
		wh.writeResponse(w, Errored(err))
	}

	wh.log.V(5).Info("Wrote response", "requestID", resp.UID, "authorized", resp.Status.Allowed)
}

// unversionedSubjectAccessReview is used to decode both v1 and v1beta1 TokenReview types.
type unversionedSubjectAccessReview struct {
	*authorizationv1.SubjectAccessReview
}
