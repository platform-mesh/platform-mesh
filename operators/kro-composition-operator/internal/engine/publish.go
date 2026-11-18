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

package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"

	krov1alpha1 "github.com/kubernetes-sigs/kro/api/v1alpha1"
	crdcompat "github.com/kubernetes-sigs/kro/pkg/graph/crd/compat"
	krometadata "github.com/kubernetes-sigs/kro/pkg/metadata"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
)

// publishComposite publishes the composite type as a bound API (APIResourceSchema +
// per-RGD APIExport + self-binding) rather than a plain CRD, which is what the
// graphql-gateway and pm-openfga-operator read. Returns true once the binding is Bound.
func (e *Engine) publishComposite(ctx context.Context, c ctrlruntimeclient.Client, crd *apiextensionsv1.CustomResourceDefinition, rgd *krov1alpha1.ResourceGraphDefinition) (bool, error) {
	name := compositeExportName(rgd.Name)

	if err := checkTypeOwnership(ctx, c, rgd, crd); err != nil {
		return false, err
	}
	if err := checkSchemaCompatibility(ctx, c, rgd, crd); err != nil {
		return false, err
	}

	// Schemas are immutable, so the content hash in the name mints a new one on any change.
	ars, err := kcpapisv1alpha1.CRDToAPIResourceSchema(crd, "kroaas-"+schemaHash(crd))
	if err != nil {
		return false, fmt.Errorf("convert CRD to APIResourceSchema: %w", err)
	}
	setRGDOwner(ars, rgd)
	applyKROOwnership(ars, rgd)
	// After the ownership labels, which an RGD must not overwrite.
	applySchemaMetadata(ars, crd)
	if err := c.Create(ctx, ars); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return false, fmt.Errorf("apply APIResourceSchema %s: %w", ars.Name, err)
		}
		// Same content hash, so only the owner UID can differ: an RGD recreated under the
		// same name. Pruning and teardown key on that UID.
		if err := adoptSchema(ctx, c, ars.Name, rgd); err != nil {
			return false, err
		}
	}

	export := &kcpapisv1alpha2.APIExport{ObjectMeta: metav1.ObjectMeta{Name: name}}
	if _, err := controllerutil.CreateOrUpdate(ctx, c, export, func() error {
		setRGDOwner(export, rgd)
		export.Spec.Resources = []kcpapisv1alpha2.ResourceSchema{{
			Name:   crd.Spec.Names.Plural,
			Group:  crd.Spec.Group,
			Schema: ars.Name,
			Storage: kcpapisv1alpha2.ResourceSchemaStorage{
				CRD: &kcpapisv1alpha2.ResourceSchemaStorageCRD{},
			},
		}}
		return nil
	}); err != nil {
		return false, fmt.Errorf("apply APIExport %s: %w", name, err)
	}

	// The export now points at the current schema, so drop schemas this RGD left behind
	// on earlier edits (APIResourceSchemas are immutable, so a schema change mints a
	// new one). Best-effort cleanup that never blocks serving the current type.
	if err := pruneStaleSchemas(ctx, c, rgd, ars.Name); err != nil {
		logf.FromContext(ctx).V(1).Info("prune stale APIResourceSchemas", "rgd", rgd.Name, "error", err.Error())
	}

	binding := &kcpapisv1alpha2.APIBinding{ObjectMeta: metav1.ObjectMeta{Name: name}}
	if _, err := controllerutil.CreateOrUpdate(ctx, c, binding, func() error {
		setRGDOwner(binding, rgd)
		// No path → kcp resolves the APIExport in the APIBinding's own logical
		// cluster, i.e. a self-binding within the consumer workspace.
		binding.Spec.Reference = kcpapisv1alpha2.BindingReference{
			Export: &kcpapisv1alpha2.ExportBindingReference{Name: name},
		}
		return nil
	}); err != nil {
		return false, fmt.Errorf("apply APIBinding %s: %w", name, err)
	}

	cur := &kcpapisv1alpha2.APIBinding{}
	if err := c.Get(ctx, types.NamespacedName{Name: name}, cur); err != nil {
		return false, err
	}
	return cur.Status.Phase == kcpapisv1alpha2.APIBindingPhaseBound, nil
}

// teardownComposite deletes the published objects, binding first and only then the export
// and schemas, so the platform's APIBinding finalizer can strip the type's authz while they
// still exist. Returns done=true once all are gone.
//
// force skips the wait when the whole workspace is terminating: the binding's finalizer
// cannot run then, so waiting would deadlock workspace deletion.
func teardownComposite(ctx context.Context, c ctrlruntimeclient.Client, rgd *krov1alpha1.ResourceGraphDefinition, force bool) (bool, error) {
	name := compositeExportName(rgd.Name)

	// 1. Binding first, so its finalizer sees the export and schema still present.
	binding := &kcpapisv1alpha2.APIBinding{}
	switch err := c.Get(ctx, types.NamespacedName{Name: name}, binding); {
	case err == nil:
		if binding.DeletionTimestamp.IsZero() {
			if delErr := c.Delete(ctx, binding); delErr != nil && !apierrors.IsNotFound(delErr) {
				return false, fmt.Errorf("delete APIBinding %s: %w", name, delErr)
			}
		}
		if !force {
			return false, nil // ordered path: wait until the binding is fully gone
		}
		// Release the stuck finalizer so the binding can go.
		if len(binding.Finalizers) > 0 {
			patch := ctrlruntimeclient.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":null}}`))
			if err := c.Patch(ctx, binding, patch); err != nil && !apierrors.IsNotFound(err) {
				return false, fmt.Errorf("clear APIBinding %s finalizers: %w", name, err)
			}
		}
	case !apierrors.IsNotFound(err):
		return false, fmt.Errorf("get APIBinding %s: %w", name, err)
	}

	// 2. Delete the APIExport, reading its identity secret ref first for step 4. kcp always
	//    sets that ref, so only its location says who owns the secret: kcp's own default is
	//    ours to delete, anywhere else belongs to whoever put it there.
	identitySecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Namespace: identitySecretNamespace,
		Name:      name,
	}}
	kcpDefaultSecret := ctrlruntimeclient.ObjectKeyFromObject(identitySecret)

	export := &kcpapisv1alpha2.APIExport{ObjectMeta: metav1.ObjectMeta{Name: name}}
	switch err := c.Get(ctx, types.NamespacedName{Name: name}, export); {
	case err == nil:
		if id := export.Spec.Identity; id != nil && id.SecretRef != nil && id.SecretRef.Name != "" {
			identitySecret.Name = id.SecretRef.Name
			if id.SecretRef.Namespace != "" {
				identitySecret.Namespace = id.SecretRef.Namespace
			}
		}
	case !apierrors.IsNotFound(err):
		return false, fmt.Errorf("get APIExport %s: %w", name, err)
	}
	if err := c.Delete(ctx, export); err != nil && !apierrors.IsNotFound(err) {
		return false, fmt.Errorf("delete APIExport %s: %w", name, err)
	}

	// 3. Delete this RGD's APIResourceSchemas.
	list := &kcpapisv1alpha1.APIResourceSchemaList{}
	if err := c.List(ctx, list); err != nil {
		return false, err
	}
	for i := range list.Items {
		ars := &list.Items[i]
		if !ownedByRGD(ars, rgd) {
			continue
		}
		if err := c.Delete(ctx, ars); err != nil && !apierrors.IsNotFound(err) {
			return false, fmt.Errorf("delete APIResourceSchema %s: %w", ars.Name, err)
		}
	}

	// 4. Delete kcp's identity secret. It carries no owner reference, so nothing else
	//    collects it and the workspace would keep one per RGD ever created.
	if key := ctrlruntimeclient.ObjectKeyFromObject(identitySecret); key == kcpDefaultSecret {
		if err := c.Delete(ctx, identitySecret); err != nil && !apierrors.IsNotFound(err) {
			return false, fmt.Errorf("delete APIExport identity secret %s: %w", key, err)
		}
	}
	return true, nil
}

// compositeExportName is the per-RGD APIExport/APIBinding name in the consumer workspace.
func compositeExportName(rgdName string) string { return "kro-" + rgdName }

// identitySecretNamespace is where kcp puts an APIExport's identity secret, named after the export.
const identitySecretNamespace = "kcp-system"

// schemaHash digests everything published in the schema, spec plus labels and annotations,
// so any change mints a new one. An immutable object cannot be relabelled in place.
func schemaHash(crd *apiextensionsv1.CustomResourceDefinition) string {
	b, _ := json.Marshal(struct {
		Spec        apiextensionsv1.CustomResourceDefinitionSpec
		Labels      map[string]string
		Annotations map[string]string
	}{crd.Spec, crd.Labels, crd.Annotations})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:12]
}

// applySchemaMetadata carries the RGD's spec.schema.metadata onto the schema. Operator-set
// keys win.
func applySchemaMetadata(ars *kcpapisv1alpha1.APIResourceSchema, crd *apiextensionsv1.CustomResourceDefinition) {
	ars.Labels = mergeMissing(ars.Labels, crd.Labels)
	ars.Annotations = mergeMissing(ars.Annotations, crd.Annotations)
}

// mergeMissing returns from with into layered on top, so into's keys win. Neither input is
// modified: these are live object label maps, which can be shared between objects.
func mergeMissing(into, from map[string]string) map[string]string {
	if len(from) == 0 {
		return into
	}
	out := make(map[string]string, len(from)+len(into))
	maps.Copy(out, from)
	maps.Copy(out, into)
	return out
}

// typeConflictError reports a composite type already published in this workspace.
type typeConflictError struct {
	gvk    string
	schema string
	owner  string // owning RGD name, empty when nothing owns the schema
}

func (e *typeConflictError) Error() string {
	if e.owner == "" {
		return fmt.Sprintf("%s is already served from APIResourceSchema %s, which no ResourceGraphDefinition owns",
			e.gvk, e.schema)
	}
	return fmt.Sprintf("%s is already published by ResourceGraphDefinition %q (APIResourceSchema %s)",
		e.gvk, e.owner, e.schema)
}

// isTypeConflict reports whether err is a refusal to publish over someone else's type.
func isTypeConflict(err error) bool {
	var t *typeConflictError
	return errors.As(err, &t)
}

// applyKROOwnership stamps the labels kro's ownership comparison reads.
func applyKROOwnership(o metav1.Object, rgd *krov1alpha1.ResourceGraphDefinition) {
	krometadata.NewKROMetaLabeler().ApplyLabels(o)
	krometadata.NewResourceGraphDefinitionLabeler(rgd).ApplyLabels(o)
}

// checkTypeOwnership refuses to publish a group/kind another RGD already publishes here.
// Without it, two RGDs with identical schemas share one content-hashed object and deleting
// either collects it out from under the other.
//
// Matching name with a different id is kro's adoption case and is allowed.
func checkTypeOwnership(
	ctx context.Context,
	c ctrlruntimeclient.Client,
	rgd *krov1alpha1.ResourceGraphDefinition,
	crd *apiextensionsv1.CustomResourceDefinition,
) error {
	desired := metav1.ObjectMeta{}
	applyKROOwnership(&desired, rgd)

	list := &kcpapisv1alpha1.APIResourceSchemaList{}
	if err := c.List(ctx, list); err != nil {
		return fmt.Errorf("list APIResourceSchemas: %w", err)
	}
	for i := range list.Items {
		ars := &list.Items[i]
		if ars.Spec.Group != crd.Spec.Group || ars.Spec.Names.Plural != crd.Spec.Names.Plural {
			continue
		}
		kroOwned, nameMatch, _ := krometadata.CompareRGDOwnership(ars.ObjectMeta, desired)
		if kroOwned && nameMatch {
			continue
		}
		return &typeConflictError{
			gvk:    crd.Spec.Names.Plural + "." + crd.Spec.Group,
			schema: ars.Name,
			owner:  ars.Labels[krometadata.ResourceGraphDefinitionNameLabel],
		}
	}
	return nil
}

// adoptSchema restamps the owner reference and kro's labels. A stale uid in either makes the
// schema an orphan to garbage collection. No-op when both match.
func adoptSchema(ctx context.Context, c ctrlruntimeclient.Client, name string, rgd *krov1alpha1.ResourceGraphDefinition) error {
	desired := metav1.ObjectMeta{}
	applyKROOwnership(&desired, rgd)

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cur := &kcpapisv1alpha1.APIResourceSchema{}
		if err := c.Get(ctx, types.NamespacedName{Name: name}, cur); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			return err
		}
		_, _, idMatch := krometadata.CompareRGDOwnership(cur.ObjectMeta, desired)
		if idMatch && ownedByRGD(cur, rgd) {
			return nil
		}
		setRGDOwner(cur, rgd)
		applyKROOwnership(cur, rgd)
		return c.Update(ctx, cur)
	})
}

// breakingSchemaChangeError reports a schema change existing instances cannot survive.
type breakingSchemaChangeError struct {
	report *crdcompat.Report
}

func (e *breakingSchemaChangeError) Error() string {
	return fmt.Sprintf("breaking schema changes: %s (set the %s annotation to publish anyway)",
		e.report, krov1alpha1.AllowBreakingChangesAnnotation)
}

// isBreakingSchemaChange reports whether err is a refused schema change.
func isBreakingSchemaChange(err error) bool {
	var b *breakingSchemaChangeError
	return errors.As(err, &b)
}

// checkSchemaCompatibility refuses a schema change that breaks the one being served, by kro's
// rules and its opt-out annotation. kcp validates each schema alone, so nothing else catches
// it. Returns nil on first publish, when there is nothing to compare against.
func checkSchemaCompatibility(
	ctx context.Context,
	c ctrlruntimeclient.Client,
	rgd *krov1alpha1.ResourceGraphDefinition,
	crd *apiextensionsv1.CustomResourceDefinition,
) error {
	if rgd.Annotations[krov1alpha1.AllowBreakingChangesAnnotation] == "true" {
		return nil
	}

	name := compositeExportName(rgd.Name)
	export := &kcpapisv1alpha2.APIExport{}
	if err := c.Get(ctx, types.NamespacedName{Name: name}, export); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get APIExport %s: %w", name, err)
	}
	if len(export.Spec.Resources) == 0 {
		return nil
	}

	servedName := export.Spec.Resources[0].Schema
	served := &kcpapisv1alpha1.APIResourceSchema{}
	if err := c.Get(ctx, types.NamespacedName{Name: servedName}, served); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get APIResourceSchema %s: %w", servedName, err)
	}

	current, err := crdVersions(served)
	if err != nil {
		return fmt.Errorf("read served schema %s: %w", servedName, err)
	}
	report, err := crdcompat.CompareVersions(current, crd.Spec.Versions)
	if err != nil {
		return fmt.Errorf("compare schema against served %s: %w", servedName, err)
	}
	if report.IsCompatible() {
		return nil
	}
	return &breakingSchemaChangeError{report: report}
}

// crdVersions converts schema versions back to CRD versions, the form kro's check takes.
func crdVersions(ars *kcpapisv1alpha1.APIResourceSchema) ([]apiextensionsv1.CustomResourceDefinitionVersion, error) {
	out := make([]apiextensionsv1.CustomResourceDefinitionVersion, 0, len(ars.Spec.Versions))
	for i := range ars.Spec.Versions {
		v := &ars.Spec.Versions[i]
		props, err := v.GetSchema()
		if err != nil {
			return nil, fmt.Errorf("version %s: %w", v.Name, err)
		}
		subresources := v.Subresources
		out = append(out, apiextensionsv1.CustomResourceDefinitionVersion{
			Name:         v.Name,
			Served:       v.Served,
			Storage:      v.Storage,
			Schema:       &apiextensionsv1.CustomResourceValidation{OpenAPIV3Schema: props},
			Subresources: &subresources,
		})
	}
	return out, nil
}

// pruneStaleSchemas deletes this RGD's schemas other than keep, left behind by earlier edits.
func pruneStaleSchemas(ctx context.Context, c ctrlruntimeclient.Client, rgd *krov1alpha1.ResourceGraphDefinition, keep string) error {
	list := &kcpapisv1alpha1.APIResourceSchemaList{}
	if err := c.List(ctx, list); err != nil {
		return err
	}
	for i := range list.Items {
		ars := &list.Items[i]
		if ars.Name == keep || !ownedByRGD(ars, rgd) {
			continue
		}
		if err := c.Delete(ctx, ars); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func ownedByRGD(o metav1.Object, rgd *krov1alpha1.ResourceGraphDefinition) bool {
	for _, ref := range o.GetOwnerReferences() {
		if ref.UID == rgd.UID {
			return true
		}
	}
	return false
}

func setRGDOwner(o metav1.Object, rgd *krov1alpha1.ResourceGraphDefinition) {
	o.SetOwnerReferences([]metav1.OwnerReference{{
		APIVersion: krov1alpha1.GroupVersion.String(),
		Kind:       "ResourceGraphDefinition",
		Name:       rgd.Name,
		UID:        rgd.UID,
	}})
}
