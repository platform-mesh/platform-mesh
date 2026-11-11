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

package quickstart

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/yaml"
)

const (
	prerequisitesChart = "platform-mesh-prerequisites"
	operatorChart      = "platform-mesh-operator"

	// gatewaySelector selects the Service of the gateway, Traefik from the
	// prerequisites chart.
	gatewaySelector = "app.kubernetes.io/name=traefik"
	// profileConfigMap is the installation profile the operator reads.
	profileConfigMap = "platform-mesh-profile"
	// signingCertSecret holds the certificates OCM signatures are
	// verified with, by signature name.
	signingCertSecret = "ocm-signing-public-cert"
	// caSecret is the Secret the operator chart's self-signed CA lives in.
	caSecret = "domain-certificate-ca"

	helmTimeout = 15 * time.Minute
)

var (
	componentGVR    = schema.GroupVersionResource{Group: "delivery.ocm.software", Version: "v1alpha1", Resource: "components"}
	platformMeshGVR = schema.GroupVersionResource{Group: "core.platform-mesh.io", Version: "v1alpha1", Resource: "platformmeshes"}
	helmReleaseGVR  = schema.GroupVersionResource{Group: "helm.toolkit.fluxcd.io", Version: "v2", Resource: "helmreleases"}
	secretGVR       = corev1.SchemeGroupVersion.WithResource("secrets")
)

// source is what gets installed, and from where.
type source struct {
	// version of the charts and of the OCM component.
	version string
	// prerequisites and operator are the chart references: oci:// or a
	// local directory.
	prerequisites, operator string
	// chartVersion is the version the charts are pulled at; empty for
	// charts in a local directory.
	chartVersion string
	// ocmRepository is the OCM repository the component is resolved
	// from; empty is Options.OCMRepository.
	ocmRepository string
	// image is the operator image; unset uses the chart's appVersion.
	image *imageValues
	// signingCert verifies the OCM signature named signature. Unset for a
	// release: the operator chart brings the certificate of those.
	signature   string
	signingCert []byte
	// details are further lines for the summary.
	details []string
}

// release is the source of the published release o.Version.
func (o *Options) release() *source {
	return &source{
		version:       o.Version,
		prerequisites: o.chartRef(prerequisitesChart),
		operator:      o.chartRef(operatorChart),
		chartVersion:  o.Version,
	}
}

// chartRef is the reference of a Platform Mesh chart in the chart repository.
func (o *Options) chartRef(name string) string {
	return o.ChartRepository + "/" + name
}

func (o *Options) prerequisitesValues(traefik bool) prerequisitesValues {
	values := prerequisitesValues{
		KeycloakOperator: keycloakOperatorValues{WatchNamespaces: namespace},
		// Controllers nothing in Platform Mesh uses.
		Flux2: flux2Values{
			ImageAutomationController: fluxControllerValues{Create: false},
			ImageReflectionController: fluxControllerValues{Create: false},
		},
	}
	if traefik {
		values.Traefik = &traefikValues{
			Enabled: true,
			Service: traefikServiceValues{
				Type: string(corev1.ServiceTypeNodePort),
				Spec: traefikServiceSpecValues{Type: string(corev1.ServiceTypeNodePort)},
			},
			Ports: traefikPortsValues{
				WebSecure: traefikPortValues{ExposedPort: gatewayPort, NodePort: gatewayNodePort},
			},
		}
	}
	return values
}

// operatorValues are the operator chart's values for installing src with a
// gateway whose Service has the given ClusterIP.
func (o *Options) operatorValues(src *source, gatewayClusterIP string) operatorValues {
	values := operatorValues{
		OCM: ocmValues{
			Enabled:       true,
			Profile:       "kind",
			Version:       o.Version,
			Repository:    o.OCMRepository,
			SignatureName: src.signature,
		},
		Gateway: gatewayValues{
			BaseDomain: o.BaseDomain,
			Port:       gatewayPort,
			ClusterIP:  gatewayClusterIP,
		},
		KCP:          kcpValues{FrontProxyPort: gatewayPort},
		Certificates: certificatesValues{SelfSigned: selfSignedValues{Enabled: true}},
		IDP:          idpValues{RegistrationAllowed: o.RegistrationAllowed},
		Image:        src.image,
	}
	if src.ocmRepository != "" {
		values.OCM.Repository = src.ocmRepository
	}
	if src.signingCert != nil {
		// applySigningCert provides the certificate instead.
		values.OCM.InstallSigningCertificate = new(false)
	}
	return values
}

// install installs the prerequisites and the operator chart, as the
// installation guide does for kind. Over an existing installation it is the
// upgrade to src.
func (o *Options) install(ctx context.Context, helm *helmClient, src *source) error {
	kube, err := o.kubeClient()
	if err != nil {
		return err
	}

	// Traefik's GatewayClass needs the Gateway API CRDs the same chart
	// brings, so the chart goes in twice: controllers and CRDs first, then
	// Traefik. A release that already has Traefik is past the first pass.
	last, err := helm.lastRelease(prerequisitesChart)
	if err != nil {
		return err
	}
	if last == nil || !traefikEnabled(last.Config) {
		err := o.step("Installing prerequisites", func() error {
			return helm.installOrUpgrade(ctx, prerequisitesChart, src.prerequisites, src.chartVersion, o.prerequisitesValues(false), true, helmTimeout)
		})
		if err != nil {
			return err
		}
	}
	var gatewayClusterIP string
	err = o.step("Installing gateway", func() error {
		err := helm.installOrUpgrade(ctx, prerequisitesChart, src.prerequisites, src.chartVersion, o.prerequisitesValues(true), true, helmTimeout)
		if err != nil {
			return err
		}
		gatewayClusterIP, err = gatewayClusterIPOf(ctx, kube)
		return err
	})
	if err != nil {
		return err
	}

	last, err = helm.lastRelease(operatorChart)
	if err != nil {
		return err
	}
	upgrading := last != nil
	values := o.operatorValues(src, gatewayClusterIP)

	return o.step("Installing Platform Mesh operator", func() error {
		if upgrading {
			// The chart refuses to upgrade over a profile it created
			// without deletion protection.
			if err := protectProfile(ctx, kube); err != nil {
				return err
			}
		}
		// Not waited for: the chart applies the PlatformMesh resource,
		// and waitReady follows what the operator makes of it.
		if err := helm.installOrUpgrade(ctx, operatorChart, src.operator, src.chartVersion, values, false, helmTimeout); err != nil {
			return err
		}
		if src.signingCert != nil {
			// Replaces the chart's own Secret, which the source turned
			// off; applied after helm, which would remove it otherwise.
			if err := applySigningCert(ctx, kube, src); err != nil {
				return err
			}
		}
		if !upgrading {
			return nil
		}
		// Helm leaves the profile alone on an upgrade: it is the
		// installation's own blueprint. A new version comes with a new
		// kind profile though, which a user following the migration
		// notes ends up with.
		manifest, err := helm.render(ctx, operatorChart, src.operator, src.chartVersion, values)
		if err != nil {
			return err
		}
		return o.applyProfile(ctx, kube, manifest)
	})
}

func (o *Options) kubeClient() (kubernetes.Interface, error) {
	restConfig, err := clientcmd.BuildConfigFromFlags("", o.Kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", o.Kubeconfig, err)
	}
	return kubernetes.NewForConfig(restConfig)
}

// protectProfile marks the profile ConfigMap as kept by helm.
// TODO(mjudeikis): Remove this once we get lifecycle handled.
func protectProfile(ctx context.Context, kube kubernetes.Interface) error {
	patch := []byte(`{"metadata":{"annotations":{"helm.sh/resource-policy":"keep"}}}`)
	_, err := kube.CoreV1().ConfigMaps(namespace).Patch(ctx, profileConfigMap, types.MergePatchType, patch, metav1.PatchOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

// applySigningCert writes the Secret the OCM Component verifies the
// signature of the source with.
func applySigningCert(ctx context.Context, kube kubernetes.Interface, src *source) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: signingCertSecret, Namespace: namespace},
		Data:       map[string][]byte{src.signature: src.signingCert},
	}
	_, err := kube.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		_, err = kube.CoreV1().Secrets(namespace).Update(ctx, secret, metav1.UpdateOptions{})
	}
	if err != nil {
		return fmt.Errorf("applying the signing certificate: %w", err)
	}
	return nil
}

// applyProfile replaces the data of the installed profile with the profile
// in manifest, a rendering of the operator chart, and has the operator
// reconcile it.
func (o *Options) applyProfile(ctx context.Context, kube kubernetes.Interface, manifest string) error {
	var profile *corev1.ConfigMap
	for doc := range strings.SplitSeq(manifest, "\n---") {
		var cm corev1.ConfigMap
		if err := yaml.Unmarshal([]byte(doc), &cm); err != nil {
			continue
		}
		if cm.Kind == "ConfigMap" && cm.Name == profileConfigMap {
			profile = &cm
			break
		}
	}
	if profile == nil {
		return fmt.Errorf("the %s chart renders no %s ConfigMap", operatorChart, profileConfigMap)
	}

	installed, err := kube.CoreV1().ConfigMaps(namespace).Get(ctx, profileConfigMap, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("reading the installed profile: %w", err)
	}
	installed.Data = profile.Data
	installed.BinaryData = profile.BinaryData
	if _, err := kube.CoreV1().ConfigMaps(namespace).Update(ctx, installed, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("applying the profile: %w", err)
	}

	restConfig, err := clientcmd.BuildConfigFromFlags("", o.Kubeconfig)
	if err != nil {
		return err
	}
	client, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return err
	}
	patch := fmt.Appendf(nil, `{"metadata":{"annotations":{"reconcile.fluxcd.io/requestedAt":%q}}}`, time.Now().UTC().Format(time.RFC3339))
	_, err = client.Resource(platformMeshGVR).Namespace(namespace).Patch(ctx, "platform-mesh", types.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("requesting a reconcile of PlatformMesh: %w", err)
	}
	return nil
}

// gatewayClusterIPOf returns the ClusterIP of the gateway Service. The kind
// profile points the Platform Mesh hostnames at it through host aliases, so
// that pods reach the gateway under the names the host uses.
func gatewayClusterIPOf(ctx context.Context, kube kubernetes.Interface) (string, error) {
	services, err := kube.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{LabelSelector: gatewaySelector})
	if err != nil {
		return "", fmt.Errorf("finding the gateway Service: %w", err)
	}
	for _, service := range services.Items {
		if ip := service.Spec.ClusterIP; ip != "" && ip != corev1.ClusterIPNone {
			return ip, nil
		}
	}
	return "", fmt.Errorf("no Service with a ClusterIP matches %q in %s", gatewaySelector, namespace)
}

func traefikEnabled(values map[string]any) bool {
	enabled, _, _ := unstructured.NestedBool(values, "traefik", "enabled")
	return enabled
}

// waitReady waits until the operator has rolled out the version and the
// portal answers, and writes the CA certificate file.
func (o *Options) waitReady(ctx context.Context) error {
	restConfig, err := clientcmd.BuildConfigFromFlags("", o.Kubeconfig)
	if err != nil {
		return fmt.Errorf("loading %s: %w", o.Kubeconfig, err)
	}
	client, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return err
	}

	err = o.poll(ctx, "Resolving OCM component "+o.Version, func(ctx context.Context) (bool, string, error) {
		component, err := client.Resource(componentGVR).Namespace(namespace).Get(ctx, "platform-mesh", metav1.GetOptions{})
		if err != nil {
			return false, err.Error(), nil //nolint:nilerr // retried, the error is the status
		}
		version, _, _ := unstructured.NestedString(component.Object, "status", "component", "version")
		if version == o.Version {
			return true, "", nil
		}
		return false, readyCondition(component), nil
	})
	if err != nil {
		return err
	}

	// From here on the charts roll out; one that keeps failing ends the wait.
	stall := newStallDetector(client, o.StallTimeout)

	err = o.poll(ctx, "Waiting for Platform Mesh to become ready", func(ctx context.Context) (bool, string, error) {
		if err := stall.check(ctx); err != nil {
			return false, "", err
		}
		pm, err := client.Resource(platformMeshGVR).Namespace(namespace).Get(ctx, "platform-mesh", metav1.GetOptions{})
		if err != nil {
			return false, err.Error(), nil //nolint:nilerr // retried, the error is the status
		}
		status, message := condition(pm, "Ready")
		return status == string(metav1.ConditionTrue), message, nil
	})
	if err != nil {
		return err
	}

	// Ready confirms the operator's subroutines, and over a running
	// installation it is true before anything changed. The charts must
	// have rolled to the version too; account-operator and portal stand
	// in for all of them.
	// TODO(mjudeikis): We need better readiness check, using PM main instance.
	for _, release := range []string{"account-operator", "portal"} {
		err = o.poll(ctx, "Rolling out "+release, func(ctx context.Context) (bool, string, error) {
			if err := stall.check(ctx); err != nil {
				return false, "", err
			}
			hr, err := client.Resource(helmReleaseGVR).Namespace(namespace).Get(ctx, release, metav1.GetOptions{})
			if err != nil {
				return false, err.Error(), nil //nolint:nilerr // retried, the error is the status
			}
			// Flux records the deployed chart in the release history,
			// as <version>+<digest>.
			history, _, _ := unstructured.NestedSlice(hr.Object, "status", "history")
			deployed := ""
			if len(history) > 0 {
				if latest, ok := history[0].(map[string]any); ok {
					deployed, _ = latest["chartVersion"].(string)
					deployed, _, _ = strings.Cut(deployed, "+")
				}
			}
			status, message := condition(hr, "Ready")
			if deployed != o.Version {
				return false, fmt.Sprintf("chart at %q: %s", deployed, message), nil
			}
			return status == string(metav1.ConditionTrue), message, nil
		})
		if err != nil {
			return err
		}
	}

	// A chart is rolled out before its pods are: wait for those too.
	kube, err := o.kubeClient()
	if err != nil {
		return err
	}
	err = o.poll(ctx, "Waiting for workloads", func(ctx context.Context) (bool, string, error) {
		if err := stall.check(ctx); err != nil {
			return false, "", err
		}
		deployments, err := kube.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return false, err.Error(), nil //nolint:nilerr // retried, the error is the status
		}
		var pending []string
		for _, d := range deployments.Items {
			want := int32(1)
			if d.Spec.Replicas != nil {
				want = *d.Spec.Replicas
			}
			status := d.Status
			if status.ObservedGeneration < d.Generation || status.UpdatedReplicas < want || status.AvailableReplicas < want || status.Replicas > status.UpdatedReplicas {
				pending = append(pending, d.Name)
			}
		}
		return len(pending) == 0, "not rolled out: " + strings.Join(pending, ", "), nil
	})
	if err != nil {
		return err
	}

	ca, err := o.writeCA(ctx, client)
	if err != nil {
		return err
	}
	return o.waitPortal(ctx, ca)
}

// writeCA writes the CA certificate of the installation to o.caFile().
func (o *Options) writeCA(ctx context.Context, client dynamic.Interface) ([]byte, error) {
	u, err := client.Resource(secretGVR).Namespace(namespace).Get(ctx, caSecret, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("reading the CA certificate: %w", err)
	}
	// The dynamic client returns Secret data base64-encoded, as on the wire.
	var secret corev1.Secret
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &secret); err != nil {
		return nil, err
	}
	ca := secret.Data[corev1.TLSCertKey]
	if len(ca) == 0 {
		return nil, fmt.Errorf("secret %s/%s has no %s", namespace, caSecret, corev1.TLSCertKey)
	}
	if err := os.WriteFile(o.caFile(), ca, 0o644); err != nil { //nolint:gosec // a public certificate
		return nil, err
	}
	return ca, nil
}

// waitPortal waits until the portal answers on the published port. It dials
// the loopback address, so that it does not depend on the host resolving the
// base domain; the name is still what the certificate is verified against.
func (o *Options) waitPortal(ctx context.Context, ca []byte) error {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return errors.New("the CA certificate is not PEM")
	}
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(gatewayPort))
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, address)
			},
		},
	}
	defer client.CloseIdleConnections()

	return o.poll(ctx, "Waiting for the portal", func(ctx context.Context) (bool, string, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.url(), nil)
		if err != nil {
			return false, "", err
		}
		resp, err := client.Do(req)
		if err != nil {
			return false, err.Error(), nil //nolint:nilerr // retried, the error is the status
		}
		_ = resp.Body.Close()
		return resp.StatusCode < http.StatusBadRequest, resp.Status, nil
	})
}

// poll runs check every few seconds, as the step what, until it reports
// done, fails, or the context ends. The status check returns is debug output,
// printed whenever it changes; the last one is part of the error.
func (o *Options) poll(ctx context.Context, what string, check func(context.Context) (done bool, status string, err error)) error {
	var last string
	err := o.step(what, func() error {
		return wait.PollUntilContextCancel(ctx, 5*time.Second, true, func(ctx context.Context) (bool, error) {
			done, status, err := check(ctx)
			if err != nil || done {
				return done, err
			}
			if status != "" && status != last {
				o.logf("%s: %s", what, status)
				last = status
			}
			return false, nil
		})
	})
	if err != nil {
		if last != "" {
			return fmt.Errorf("%s: %w (last status: %s)", what, err, last)
		}
		return fmt.Errorf("%s: %w", what, err)
	}
	return nil
}

// condition returns the status and a "reason: message" text of a condition.
func condition(obj *unstructured.Unstructured, conditionType string) (status, text string) {
	conditions, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	for _, c := range conditions {
		c, ok := c.(map[string]any)
		if !ok || c["type"] != conditionType {
			continue
		}
		status, _ = c["status"].(string)
		reason, _ := c["reason"].(string)
		message, _ := c["message"].(string)
		switch {
		case reason != "" && message != "":
			return status, reason + ": " + message
		default:
			return status, reason + message
		}
	}
	return "", "no " + conditionType + " condition yet"
}

func readyCondition(obj *unstructured.Unstructured) string {
	_, text := condition(obj, "Ready")
	return text
}
