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

package identityprovider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"time"

	pmidentityv1alpha1 "go.platform-mesh.io/apis/identity/v1alpha1"
	"go.platform-mesh.io/pm-keycloak-operator/pkg/directory"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	webhookadmission "sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/kcp-dev/logicalcluster/v3"
)

// AdmissionHandler is the optional, read-only provider policy endpoint. Shared
// admission and RBAC still enforce administrative approval and immutable fields.
type AdmissionHandler struct {
	Classes, Private ctrlruntimeclient.Reader
	ClassName        string
	RuntimeNamespace string
	Timeout          time.Duration
}

func (h *AdmissionHandler) Handle(ctx context.Context, req webhookadmission.Request) webhookadmission.Response {
	tenantResource := req.Kind.Kind == "IdentityTenant" && req.Resource.Resource == "identitytenants"
	invitationResource := req.Kind.Kind == "Invitation" && req.Resource.Resource == "invitations"
	if req.Kind.Group != pmidentityv1alpha1.GroupName || req.Kind.Version != pmidentityv1alpha1.GroupVersion ||
		(!tenantResource && !invitationResource) ||
		req.Resource.Group != req.Kind.Group || req.Resource.Version != req.Kind.Version {
		return webhookadmission.Denied("unsupported default-provider validation resource")
	}
	if req.SubResource == "status" || req.Operation == admissionv1.Delete {
		return webhookadmission.Allowed("lifecycle operations do not require provider availability")
	}
	if req.SubResource != "" || (req.Operation != admissionv1.Create && req.Operation != admissionv1.Update) {
		return webhookadmission.Denied("unsupported default-provider validation operation")
	}
	if req.Kind.Kind == "Invitation" {
		return h.invitation(ctx, req)
	}
	var tenant *pmidentityv1alpha1.IdentityTenant
	if err := json.Unmarshal(req.Object.Raw, &tenant); err != nil || tenant == nil {
		return webhookadmission.Errored(http.StatusBadRequest, errors.New("invalid identity tenant"))
	}
	if req.Operation == admissionv1.Update {
		var old *pmidentityv1alpha1.IdentityTenant
		if len(req.OldObject.Raw) == 0 || json.Unmarshal(req.OldObject.Raw, &old) != nil || old == nil {
			return webhookadmission.Errored(http.StatusBadRequest, errors.New("previous identity tenant is required"))
		}
		if equality.Semantic.DeepEqual(old.Spec, tenant.Spec) {
			return webhookadmission.Allowed("metadata-only update")
		}
	}
	if errs := pmidentityv1alpha1.ValidateTenant(tenant); len(errs) != 0 {
		return webhookadmission.Errored(http.StatusUnprocessableEntity, errs.ToAggregate())
	}
	if tenant.Spec.ProviderClassRef != h.ClassName || !validRealm(tenant.Name) {
		return webhookadmission.Denied("tenant must select the configured class and a nonreserved realm name")
	}
	if tenant.Spec.AdoptionRef != "" {
		return webhookadmission.Denied("external adoption requires the coordinated migration protocol")
	}
	if h.Classes == nil || h.Private == nil || h.RuntimeNamespace == "" || h.Timeout <= 0 {
		return webhookadmission.Errored(http.StatusServiceUnavailable, errors.New("trusted default-provider configuration is unavailable"))
	}
	bounded, cancel := context.WithTimeout(ctx, h.Timeout)
	defer cancel()
	class := &pmidentityv1alpha1.IdentityProviderClass{}
	if err := h.Classes.Get(bounded, ctrlruntimeclient.ObjectKey{Name: h.ClassName}, class); err != nil ||
		class.UID == "" || class.Spec.ControllerName != directory.KeycloakControllerName ||
		class.Spec.AuthenticationPolicy.IssuerMode != pmidentityv1alpha1.IssuerModeDedicated {
		return webhookadmission.Errored(http.StatusServiceUnavailable, errors.New("selected default-provider class is unavailable"))
	}
	config, err := LoadConfiguration(bounded, h.Classes, h.Private, class, h.RuntimeNamespace, h.Timeout)
	if err != nil {
		return webhookadmission.Errored(http.StatusServiceUnavailable, errors.New("default-provider validation configuration is unavailable"))
	}
	defer config.Close()
	var realm struct {
		Attributes map[string]string `json:"attributes"`
	}
	err = config.Backend.request(bounded, config.Backend.admin, http.MethodGet, config.Backend.realmURL(tenant.Name), nil, &realm)
	if errors.Is(err, ErrRemoteNotFound) {
		return webhookadmission.Allowed("realm name is available")
	}
	if err != nil {
		return webhookadmission.Errored(http.StatusServiceUnavailable, errors.New("default-provider realm validation is unavailable"))
	}
	cluster := logicalcluster.From(tenant)
	if req.Operation == admissionv1.Update && cluster.IsValid() && tenant.UID != "" &&
		realm.Attributes[ownerAttribute] == cluster.String()+"/"+string(tenant.UID) {
		return webhookadmission.Allowed("existing realm is owned by this tenant")
	}
	return webhookadmission.Denied("realm already exists and cannot be implicitly adopted")
}

func (h *AdmissionHandler) invitation(ctx context.Context, req webhookadmission.Request) webhookadmission.Response {
	var invitation *pmidentityv1alpha1.Invitation
	if json.Unmarshal(req.Object.Raw, &invitation) != nil || invitation == nil {
		return webhookadmission.Errored(http.StatusBadRequest, errors.New("invalid invitation"))
	}
	if req.Operation == admissionv1.Update {
		var old *pmidentityv1alpha1.Invitation
		if json.Unmarshal(req.OldObject.Raw, &old) != nil || old == nil {
			return webhookadmission.Errored(http.StatusBadRequest, errors.New("previous invitation is required"))
		}
		if !equality.Semantic.DeepEqual(old.Spec, invitation.Spec) {
			return webhookadmission.Denied("invitation spec is immutable")
		}
		return webhookadmission.Allowed("metadata-only update")
	}
	if errs := pmidentityv1alpha1.ValidateInvitation(invitation); len(errs) != 0 {
		return webhookadmission.Errored(http.StatusUnprocessableEntity, errs.ToAggregate())
	}
	if h.Classes == nil || h.Timeout <= 0 {
		return webhookadmission.Errored(http.StatusServiceUnavailable, errors.New("trusted provider class is unavailable"))
	}
	bounded, cancel := context.WithTimeout(ctx, h.Timeout)
	defer cancel()
	class := &pmidentityv1alpha1.IdentityProviderClass{}
	if err := h.Classes.Get(bounded, ctrlruntimeclient.ObjectKey{Name: h.ClassName}, class); err != nil ||
		class.UID == "" || class.Spec.ControllerName != directory.KeycloakControllerName ||
		!slices.Contains(class.Status.Capabilities, pmidentityv1alpha1.IdentityCapabilityInvitations) {
		return webhookadmission.Denied("selected default provider is not configured for invitation delivery")
	}
	return webhookadmission.Allowed("invitation target is resolved through the trusted identity projection during reconciliation")
}
