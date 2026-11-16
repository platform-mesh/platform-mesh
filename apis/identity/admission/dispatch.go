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

package admission

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	pmidentityv1alpha1 "go.platform-mesh.io/apis/identity/v1alpha1"
	"go.platform-mesh.io/golang-commons/identity"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	webhookadmission "sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const maxReviewBytes = 2 << 20

// HTTPDispatcher sends standard admission.k8s.io/v1 AdmissionReviews. Its
// factory resolves endpoint trust and client certificates in the installation's
// runtime namespace, never a tenant-selected namespace.
type HTTPDispatcher struct {
	ClientForClass func(context.Context, *pmidentityv1alpha1.IdentityProviderClass) (*http.Client, error)
	Timeout        time.Duration
}

func (d *HTTPDispatcher) Validate(ctx context.Context, class *pmidentityv1alpha1.IdentityProviderClass, req webhookadmission.Request) (webhookadmission.Response, error) {
	zero := webhookadmission.Response{}
	if req.UID == "" {
		return zero, errors.New("provider validation requires a request UID")
	}
	if class == nil || class.Spec.Validation == nil || d.ClientForClass == nil || d.Timeout <= 0 {
		return zero, errors.New("provider validation endpoint, private transport factory and positive timeout are required")
	}
	endpoint := class.Spec.Validation.URL
	if err := (identity.Principal{Issuer: endpoint, Subject: "validator"}).Validate(); err != nil {
		return zero, errors.New("provider validator must use an administrator-configured HTTPS endpoint")
	}
	client, err := d.ClientForClass(ctx, class)
	if err != nil {
		return zero, errors.New("provider validator transport configuration is unavailable")
	}
	if client == nil || client.Timeout <= 0 {
		return zero, errors.New("bounded provider validator HTTP client is required")
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.TLSClientConfig == nil || transport.TLSClientConfig.InsecureSkipVerify ||
		transport.TLSClientConfig.RootCAs == nil || len(transport.TLSClientConfig.Certificates) == 0 {
		return zero, errors.New("provider validator requires explicit server trust and a mutual TLS workload certificate")
	}
	bounded, cancel := context.WithTimeout(ctx, d.Timeout)
	defer cancel()
	review := admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{APIVersion: admissionv1.SchemeGroupVersion.String(), Kind: "AdmissionReview"},
		Request:  &req.AdmissionRequest,
	}
	data, err := json.Marshal(review)
	if err != nil || len(data) > maxReviewBytes {
		return zero, errors.New("provider validation request is invalid or oversized")
	}
	request, err := http.NewRequestWithContext(bounded, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return zero, errors.New("invalid provider validation request")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cache-Control", "no-store")
	direct := *client
	defer direct.CloseIdleConnections()
	direct.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return errors.New("provider validator redirects are forbidden")
	}
	response, err := direct.Do(request)
	if err != nil {
		return zero, errors.New("provider validator is unavailable")
	}
	defer response.Body.Close() //nolint:errcheck
	if response.StatusCode != http.StatusOK {
		return zero, errors.New("provider validator returned an unsuccessful response")
	}
	data, err = io.ReadAll(io.LimitReader(response.Body, maxReviewBytes+1))
	if err != nil || len(data) > maxReviewBytes {
		return zero, errors.New("provider validation response exceeds the size limit or could not be read")
	}
	var result admissionv1.AdmissionReview
	if err := json.Unmarshal(data, &result); err != nil || result.Response == nil || result.Request != nil ||
		result.Kind != "AdmissionReview" || result.APIVersion != admissionv1.SchemeGroupVersion.String() ||
		req.UID == "" || result.Response.UID != req.UID {
		return zero, errors.New("provider validator returned an invalid admission response")
	}
	if len(result.Response.Patch) != 0 || result.Response.PatchType != nil {
		return zero, errors.New("provider validation must not mutate identity resources")
	}
	return webhookadmission.Response{AdmissionResponse: *result.Response}, nil
}
