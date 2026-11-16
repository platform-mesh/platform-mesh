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

// Package admission implements provider-neutral identity admission. API binding
// and RBAC remain responsible for authenticating admission callers and writers.
package admission

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"

	pmidentityv1alpha1 "go.platform-mesh.io/apis/identity/v1alpha1"

	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	webhookadmission "sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type Dispatcher interface {
	Validate(context.Context, *pmidentityv1alpha1.IdentityProviderClass, webhookadmission.Request) (webhookadmission.Response, error)
}

// Handler resolves classes only through the installation-owned reader.
// Privileged approval is intentionally injected, not inferred from email,
// resource annotations or a user-selected class workspace.
type Handler struct {
	Classes     ctrlruntimeclient.Reader
	ActiveClass string
	Dispatcher  Dispatcher
	Privileged  func(context.Context, authenticationv1.UserInfo) (bool, error)
}

var _ webhookadmission.Handler = (*Handler)(nil)

func (h *Handler) Handle(ctx context.Context, req webhookadmission.Request) webhookadmission.Response {
	if req.Kind.Group != pmidentityv1alpha1.GroupName || req.Kind.Version != pmidentityv1alpha1.GroupVersion {
		return webhookadmission.Denied("unsupported identity API version")
	}
	resources := map[string]string{
		"IdentityTenant": "identitytenants", "IdentityProviderClass": "identityproviderclasses", "Invitation": "invitations",
	}
	resource, supported := resources[req.Kind.Kind]
	if !supported || req.Resource.Group != req.Kind.Group || req.Resource.Version != req.Kind.Version || req.Resource.Resource != resource {
		return webhookadmission.Denied("unsupported identity resource")
	}
	if req.SubResource == "status" || req.Operation == admissionv1.Delete {
		return webhookadmission.Allowed("lifecycle operations do not require provider availability")
	}
	if req.SubResource != "" || (req.Operation != admissionv1.Create && req.Operation != admissionv1.Update) {
		return webhookadmission.Denied("unsupported identity operation")
	}
	className := h.ActiveClass
	privileged := req.Kind.Kind == "IdentityProviderClass"
	switch req.Kind.Kind {
	case "IdentityTenant":
		var current, old pmidentityv1alpha1.IdentityTenant
		if err := decode(req, &current, &old); err != nil {
			return webhookadmission.Errored(http.StatusBadRequest, err)
		}
		if req.Operation == admissionv1.Update && equality.Semantic.DeepEqual(old.Spec, current.Spec) {
			return webhookadmission.Allowed("metadata-only update")
		}
		if errs := pmidentityv1alpha1.ValidateTenant(&current); len(errs) != 0 {
			return webhookadmission.Errored(http.StatusUnprocessableEntity, errs.ToAggregate())
		}
		if req.Operation == admissionv1.Update &&
			(current.Spec.ProviderClassRef != old.Spec.ProviderClassRef || current.Spec.Purpose != old.Spec.Purpose ||
				current.Spec.Organization != old.Spec.Organization || current.Spec.AdoptionRef != old.Spec.AdoptionRef) {
			return webhookadmission.Denied("provider binding, purpose, organization and adoption reference are immutable")
		}
		className = current.Spec.ProviderClassRef
		privileged = current.Spec.AdoptionRef != "" ||
			(current.Spec.DeletionPolicy == pmidentityv1alpha1.DeletionPolicyRetain &&
				(req.Operation == admissionv1.Create || old.Spec.DeletionPolicy != current.Spec.DeletionPolicy))
	case "Invitation":
		var current, old pmidentityv1alpha1.Invitation
		if err := decode(req, &current, &old); err != nil {
			return webhookadmission.Errored(http.StatusBadRequest, err)
		}
		if req.Operation == admissionv1.Update {
			if !equality.Semantic.DeepEqual(old.Spec, current.Spec) {
				return webhookadmission.Denied("invitation spec is immutable")
			}
			return webhookadmission.Allowed("metadata-only update")
		}
		if errs := pmidentityv1alpha1.ValidateInvitation(&current); len(errs) != 0 {
			return webhookadmission.Errored(http.StatusUnprocessableEntity, errs.ToAggregate())
		}
	case "IdentityProviderClass":
		var current, old pmidentityv1alpha1.IdentityProviderClass
		if err := decode(req, &current, &old); err != nil {
			return webhookadmission.Errored(http.StatusBadRequest, err)
		}
		if req.Operation == admissionv1.Update && equality.Semantic.DeepEqual(old.Spec, current.Spec) {
			return webhookadmission.Allowed("metadata-only update")
		}
		if errs := pmidentityv1alpha1.ValidateProviderClass(&current); len(errs) != 0 {
			return webhookadmission.Errored(http.StatusUnprocessableEntity, errs.ToAggregate())
		}
		if req.Operation == admissionv1.Update &&
			(current.Spec.ControllerName != old.Spec.ControllerName ||
				!equality.Semantic.DeepEqual(current.Spec.ConfigurationRef, old.Spec.ConfigurationRef) ||
				!equality.Semantic.DeepEqual(current.Spec.AuthenticationPolicy, old.Spec.AuthenticationPolicy)) {
			return webhookadmission.Denied("provider controller, configuration reference and authentication policy are immutable")
		}
		className = current.Name
	}
	if className == "" || className != h.ActiveClass {
		return webhookadmission.Denied("only the installation-selected provider class is supported")
	}
	if privileged {
		if h.Privileged == nil {
			return webhookadmission.Denied("explicit administrative authorization is required")
		}
		allowed, err := h.Privileged(ctx, req.UserInfo)
		if err != nil {
			return webhookadmission.Errored(http.StatusServiceUnavailable, err)
		}
		if !allowed {
			return webhookadmission.Denied("explicit administrative authorization is required")
		}
	}
	if req.Kind.Kind == "IdentityProviderClass" {
		return webhookadmission.Allowed("administrator-owned provider class")
	}
	if h.Classes == nil {
		return webhookadmission.Errored(http.StatusServiceUnavailable, errors.New("trusted provider class reader is unavailable"))
	}
	class := &pmidentityv1alpha1.IdentityProviderClass{}
	if err := h.Classes.Get(ctx, ctrlruntimeclient.ObjectKey{Name: className}, class); err != nil {
		return webhookadmission.Errored(http.StatusServiceUnavailable, fmt.Errorf("resolve installation provider class: %w", err))
	}
	if errs := pmidentityv1alpha1.ValidateProviderClass(class); len(errs) != 0 {
		return webhookadmission.Errored(http.StatusServiceUnavailable, errors.New("installation provider class is invalid"))
	}
	if req.Kind.Kind == "Invitation" &&
		(!meta.IsStatusConditionTrue(class.Status.Conditions, "Ready") ||
			class.Status.ObservedGeneration != class.Generation ||
			!slices.Contains(class.Status.Capabilities, pmidentityv1alpha1.IdentityCapabilityInvitations)) {
		return webhookadmission.Denied("selected provider has not established invitation capability")
	}
	if class.Spec.Validation == nil {
		return webhookadmission.Allowed("shared identity validation passed")
	}
	if h.Dispatcher == nil {
		return webhookadmission.Errored(http.StatusServiceUnavailable, errors.New("configured provider validator is unavailable"))
	}
	response, err := h.Dispatcher.Validate(ctx, class, req)
	if err != nil {
		return webhookadmission.Errored(http.StatusServiceUnavailable, err)
	}
	return response
}

func decode(req webhookadmission.Request, current, previous any) error {
	if len(req.Object.Raw) == 0 || bytes.Equal(bytes.TrimSpace(req.Object.Raw), []byte("null")) {
		return errors.New("identity admission requires an object")
	}
	if err := json.Unmarshal(req.Object.Raw, current); err != nil {
		return errors.New("identity admission object is invalid")
	}
	if req.Operation == admissionv1.Update {
		if len(req.OldObject.Raw) == 0 || bytes.Equal(bytes.TrimSpace(req.OldObject.Raw), []byte("null")) {
			return errors.New("identity update requires the previous object")
		}
		if err := json.Unmarshal(req.OldObject.Raw, previous); err != nil {
			return errors.New("previous identity object is invalid")
		}
	}
	return nil
}
