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
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	prerequisitesChart = "platform-mesh-prerequisites"
	operatorChart      = "platform-mesh-operator"

	// gatewaySelector selects the Service of the gateway, Traefik from the
	// prerequisites chart.
	gatewaySelector = "app.kubernetes.io/name=traefik"
	// caSecret is the Secret the operator chart's self-signed CA lives in.
	caSecret = "domain-certificate-ca"

	helmTimeout = 15 * time.Minute
)

var (
	componentGVR    = schema.GroupVersionResource{Group: "delivery.ocm.software", Version: "v1alpha1", Resource: "components"}
	platformMeshGVR = schema.GroupVersionResource{Group: "core.platform-mesh.io", Version: "v1alpha1", Resource: "platformmeshes"}
	secretGVR       = corev1.SchemeGroupVersion.WithResource("secrets")
)

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

// operatorValues are the operator chart's values for a gateway whose Service
// has the given ClusterIP.
func (o *Options) operatorValues(gatewayClusterIP string) operatorValues {
	return operatorValues{
		OCM: ocmValues{
			Enabled:    true,
			Profile:    "kind",
			Version:    o.Version,
			Repository: o.OCMRepository,
		},
		Gateway: gatewayValues{
			BaseDomain: o.BaseDomain,
			Port:       gatewayPort,
			ClusterIP:  gatewayClusterIP,
		},
		KCP:          kcpValues{FrontProxyPort: gatewayPort},
		Certificates: certificatesValues{SelfSigned: selfSignedValues{Enabled: true}},
		IDP:          idpValues{RegistrationAllowed: o.RegistrationAllowed},
	}
}

// install installs the prerequisites and the operator chart, as the
// installation guide does for kind.
func (o *Options) install(ctx context.Context, helm *helmClient) error {
	// Traefik's GatewayClass needs the Gateway API CRDs the same chart
	// brings, so the chart goes in twice: controllers and CRDs first, then
	// Traefik. A release that already has Traefik is past the first pass.
	last, err := helm.lastRelease(prerequisitesChart)
	if err != nil {
		return err
	}
	if last == nil || !traefikEnabled(last.Config) {
		err := o.step("Installing prerequisites", func() error {
			return helm.installOrUpgrade(ctx, prerequisitesChart, o.chartRef(prerequisitesChart), o.Version, o.prerequisitesValues(false), true, helmTimeout)
		})
		if err != nil {
			return err
		}
	}
	var gatewayClusterIP string
	err = o.step("Installing gateway", func() error {
		err := helm.installOrUpgrade(ctx, prerequisitesChart, o.chartRef(prerequisitesChart), o.Version, o.prerequisitesValues(true), true, helmTimeout)
		if err != nil {
			return err
		}
		gatewayClusterIP, err = o.gatewayClusterIP(ctx)
		return err
	})
	if err != nil {
		return err
	}

	// Not waited for: the chart applies the PlatformMesh resource, and
	// waitReady follows what the operator makes of it.
	return o.step("Installing Platform Mesh operator", func() error {
		return helm.installOrUpgrade(ctx, operatorChart, o.chartRef(operatorChart), o.Version, o.operatorValues(gatewayClusterIP), false, helmTimeout)
	})
}

// gatewayClusterIP returns the ClusterIP of the gateway Service. The kind
// profile points the Platform Mesh hostnames at it through host aliases, so
// that pods reach the gateway under the names the host uses.
func (o *Options) gatewayClusterIP(ctx context.Context) (string, error) {
	restConfig, err := clientcmd.BuildConfigFromFlags("", o.Kubeconfig)
	if err != nil {
		return "", fmt.Errorf("loading %s: %w", o.Kubeconfig, err)
	}
	kube, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return "", err
	}
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

	err = o.poll(ctx, "Waiting for Platform Mesh to become ready", func(ctx context.Context) (bool, string, error) {
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
