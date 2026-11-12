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

// Package pki collects and restores the kcp certificates, issuers and their secrets.
package pki

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"sort"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"

	archivev1alpha1 "go.platform-mesh.io/apis/backup/archive/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/kcp"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	kcpoperatorv1alpha1 "github.com/kcp-dev/kcp-operator/sdk/apis/operator/v1alpha1"
)

const certificateNameAnnotation = "cert-manager.io/certificate-name"

// Collect returns the Issuers owned by kcp-operator, the Certificates owned by kcp-operator or
// issued by one of these Issuers, the Issuers they use, and the Secrets of the Certificates.
func Collect(ctx context.Context, cl ctrlruntimeclient.Client, namespace string) (*archivev1alpha1.Bundle, error) {
	issuers := &certmanagerv1.IssuerList{}
	if err := cl.List(ctx, issuers, ctrlruntimeclient.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("listing issuers: %w", err)
	}
	certs := &certmanagerv1.CertificateList{}
	if err := cl.List(ctx, certs, ctrlruntimeclient.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("listing certificates: %w", err)
	}

	kcpIssuers := map[string]bool{}
	for _, i := range issuers.Items {
		if kcp.IsOwned(&i) {
			kcpIssuers[i.Name] = true
		}
	}

	b := archivev1alpha1.NewBundle()
	usedIssuers := map[string]bool{}
	for _, c := range certs.Items {
		ref := c.Spec.IssuerRef
		localIssuer := ref.Kind == "" || ref.Kind == certmanagerv1.IssuerKind
		if !kcp.IsOwned(&c) && (!localIssuer || !kcpIssuers[ref.Name]) {
			continue
		}
		u, err := toUnstructured(clean(c), certmanagerv1.SchemeGroupVersion.WithKind(certmanagerv1.CertificateKind))
		if err != nil {
			return nil, err
		}
		b.Certificates = append(b.Certificates, u)
		if localIssuer {
			usedIssuers[ref.Name] = true
		}

		secret := corev1.Secret{}
		if err := cl.Get(ctx, types.NamespacedName{Namespace: namespace, Name: c.Spec.SecretName}, &secret); err != nil {
			return nil, fmt.Errorf("getting secret of certificate %s: %w", c.Name, err)
		}
		b.Secrets = append(b.Secrets, cleanSecret(secret))
	}
	for _, i := range issuers.Items {
		if kcpIssuers[i.Name] || usedIssuers[i.Name] {
			u, err := toUnstructured(clean(i), certmanagerv1.SchemeGroupVersion.WithKind(certmanagerv1.IssuerKind))
			if err != nil {
				return nil, err
			}
			b.Issuers = append(b.Issuers, u)
		}
	}
	if len(b.Certificates) == 0 {
		return nil, fmt.Errorf("no kcp certificates found in namespace %s", namespace)
	}
	kubeconfigs := &kcpoperatorv1alpha1.KubeconfigList{}
	if err := cl.List(ctx, kubeconfigs, ctrlruntimeclient.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("listing kubeconfigs: %w", err)
	}
	for _, k := range kubeconfigs.Items {
		k = clean(k)
		k.TypeMeta = metav1.TypeMeta{APIVersion: kcpoperatorv1alpha1.SchemeGroupVersion.String(), Kind: "Kubeconfig"}
		b.Kubeconfigs = append(b.Kubeconfigs, k)
	}
	sort.Slice(b.Secrets, func(i, j int) bool { return b.Secrets[i].Name < b.Secrets[j].Name })
	return b, nil
}

// RestoreSecrets writes the secrets back. The CertificateRequests of the Certificates are
// deleted first, otherwise cert-manager sees a key that does not match the last request and
// issues a new certificate.
func RestoreSecrets(ctx context.Context, cl ctrlruntimeclient.Client, namespace string, b *archivev1alpha1.Bundle) error {
	certNames := map[string]bool{}
	for _, c := range b.Certificates {
		certNames[c.GetName()] = true
	}
	requests := &certmanagerv1.CertificateRequestList{}
	if err := cl.List(ctx, requests, ctrlruntimeclient.InNamespace(namespace)); err != nil {
		return fmt.Errorf("listing certificaterequests: %w", err)
	}
	for i := range requests.Items {
		r := &requests.Items[i]
		if !certNames[r.Annotations[certificateNameAnnotation]] {
			continue
		}
		if err := cl.Delete(ctx, r); ctrlruntimeclient.IgnoreNotFound(err) != nil {
			return fmt.Errorf("deleting certificaterequest %s: %w", r.Name, err)
		}
	}

	for _, s := range b.Secrets {
		want := s.DeepCopy()
		want.Namespace = namespace
		existing := &corev1.Secret{}
		err := cl.Get(ctx, types.NamespacedName{Namespace: namespace, Name: s.Name}, existing)
		switch {
		case apierrors.IsNotFound(err):
			if err := cl.Create(ctx, want); err != nil {
				return fmt.Errorf("creating secret %s: %w", s.Name, err)
			}
		case err != nil:
			return fmt.Errorf("getting secret %s: %w", s.Name, err)
		default:
			existing.Labels = want.Labels
			existing.Annotations = want.Annotations
			existing.Data = want.Data
			if err := cl.Update(ctx, existing); err != nil {
				return fmt.Errorf("updating secret %s: %w", s.Name, err)
			}
		}
	}
	return nil
}

// RestoreCertificates creates the Issuers, Certificates and Kubeconfigs that are missing.
// Existing ones are not changed, kcp-operator owns their spec.
func RestoreCertificates(ctx context.Context, cl ctrlruntimeclient.Client, namespace string, b *archivev1alpha1.Bundle) (int, error) {
	var objs []ctrlruntimeclient.Object
	for _, list := range [][]unstructured.Unstructured{b.Issuers, b.Certificates} {
		for _, o := range list {
			objs = append(objs, o.DeepCopy())
		}
	}
	for _, k := range b.Kubeconfigs {
		objs = append(objs, k.DeepCopy())
	}
	created := 0
	for _, obj := range objs {
		kind := obj.GetObjectKind().GroupVersionKind().Kind
		obj.SetNamespace(namespace)
		ok, err := createIfMissing(ctx, cl, obj)
		if err != nil {
			return created, fmt.Errorf("%s %s: %w", kind, obj.GetName(), err)
		}
		if ok {
			created++
		}
	}
	return created, nil
}

func createIfMissing(ctx context.Context, cl ctrlruntimeclient.Client, obj ctrlruntimeclient.Object) (bool, error) {
	if err := cl.Get(ctx, ctrlruntimeclient.ObjectKeyFromObject(obj), obj.DeepCopyObject().(ctrlruntimeclient.Object)); err == nil {
		return false, nil
	} else if !apierrors.IsNotFound(err) {
		return false, err
	}
	refs, err := liveOwners(ctx, cl, obj.GetNamespace(), obj.GetOwnerReferences())
	if err != nil {
		return false, err
	}
	obj.SetOwnerReferences(refs)
	if err := cl.Create(ctx, obj); err != nil {
		return false, err
	}
	return true, nil
}

// liveOwners drops owner references to objects that do not exist anymore with the same uid,
// the garbage collector would delete the restored object otherwise.
func liveOwners(ctx context.Context, cl ctrlruntimeclient.Client, namespace string, refs []metav1.OwnerReference) ([]metav1.OwnerReference, error) {
	var out []metav1.OwnerReference
	for _, ref := range refs {
		uid, err := kcp.OwnerUID(ctx, cl, namespace, ref)
		if err != nil {
			return nil, err
		}
		if uid == ref.UID {
			out = append(out, ref)
		}
	}
	return out, nil
}

type object[T any] interface {
	*T
	ctrlruntimeclient.Object
}

func clean[T any, P object[T]](in T) T {
	p := P(&in)
	p.SetUID("")
	p.SetResourceVersion("")
	p.SetGeneration(0)
	p.SetCreationTimestamp(metav1.Time{})
	p.SetManagedFields(nil)
	switch o := any(p).(type) {
	case *certmanagerv1.Certificate:
		o.Status = certmanagerv1.CertificateStatus{}
	case *certmanagerv1.Issuer:
		o.Status = certmanagerv1.IssuerStatus{}
	case *kcpoperatorv1alpha1.Kubeconfig:
		o.Status = kcpoperatorv1alpha1.KubeconfigStatus{}
	}
	return in
}

func toUnstructured[T any](obj T, gvk schema.GroupVersionKind) (unstructured.Unstructured, error) {
	m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&obj)
	if err != nil {
		return unstructured.Unstructured{}, err
	}
	u := unstructured.Unstructured{Object: m}
	u.SetGroupVersionKind(gvk)
	delete(u.Object, "status")
	return u, nil
}

func cleanSecret(s corev1.Secret) corev1.Secret {
	s = clean(s)
	s.OwnerReferences = nil
	return s
}

// Settled returns "" when every secret has the data from the bundle and no kcp Certificate is
// being issued. Else it says what is still different.
func Settled(ctx context.Context, cl ctrlruntimeclient.Client, namespace string, b *archivev1alpha1.Bundle) (string, error) {
	for _, s := range b.Secrets {
		got := &corev1.Secret{}
		if err := cl.Get(ctx, types.NamespacedName{Namespace: namespace, Name: s.Name}, got); err != nil {
			if apierrors.IsNotFound(err) {
				return "secret " + s.Name + " is missing", nil
			}
			return "", err
		}
		if !maps.EqualFunc(got.Data, s.Data, bytes.Equal) {
			return "secret " + s.Name + " was changed", nil
		}
	}
	for _, c := range b.Certificates {
		got := &certmanagerv1.Certificate{}
		if err := cl.Get(ctx, types.NamespacedName{Namespace: namespace, Name: c.GetName()}, got); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return "", err
		}
		for _, cond := range got.Status.Conditions {
			if cond.Type == certmanagerv1.CertificateConditionIssuing && cond.Status == cmmeta.ConditionTrue {
				return "certificate " + c.GetName() + " is being issued", nil
			}
		}
	}
	return "", nil
}
