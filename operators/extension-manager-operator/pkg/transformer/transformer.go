package transformer

import (
	"go.platform-mesh.io/apis/ui/v1alpha1"
	"go.platform-mesh.io/extension-manager-operator/pkg/validation"
)

type ContentConfigurationTransformer interface {
	Transform(contentConfiguration *validation.ContentConfiguration, instance *v1alpha1.ContentConfiguration) error
}
