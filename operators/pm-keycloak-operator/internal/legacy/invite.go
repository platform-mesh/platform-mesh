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

package legacy

import (
	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmkeycloakv1alpha1 "go.platform-mesh.io/apis/keycloak/v1alpha1"
	"go.platform-mesh.io/subroutines"

	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// Invite adapts sub to the deprecated core.platform-mesh.io Invite.
func Invite(sub subroutines.Subroutine) *Subroutine {
	return &Subroutine{
		inner:   sub,
		convert: convertInvite,
	}
}

func convertInvite(obj ctrlruntimeclient.Object) (ctrlruntimeclient.Object, func(), error) {
	//nolint:staticcheck // legacy kind served on purpose
	legacy, ok := obj.(*pmcorev1alpha1.Invite)
	if !ok {
		return nil, nil, wrongType(obj, "*core.Invite")
	}
	current := inviteToCurrent(legacy)
	return current, func() { inviteFromCurrent(current, legacy) }, nil
}

//nolint:staticcheck // legacy kind served on purpose
func inviteToCurrent(in *pmcorev1alpha1.Invite) *pmkeycloakv1alpha1.Invite {
	out := &pmkeycloakv1alpha1.Invite{
		ObjectMeta: *in.ObjectMeta.DeepCopy(),
		Spec:       pmkeycloakv1alpha1.InviteSpec{Email: in.Spec.Email},
		Status:     pmkeycloakv1alpha1.InviteStatus{Conditions: in.Status.DeepCopy().Conditions},
	}
	out.TypeMeta.APIVersion = pmkeycloakv1alpha1.SchemeGroupVersion.String()
	out.TypeMeta.Kind = "Invite"
	return out
}

//nolint:staticcheck // legacy kind served on purpose
func inviteFromCurrent(in *pmkeycloakv1alpha1.Invite, out *pmcorev1alpha1.Invite) {
	out.ObjectMeta = *in.ObjectMeta.DeepCopy()
	out.Status.Conditions = in.Status.DeepCopy().Conditions
}
