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

package subroutines

import (
	"context"
	"fmt"
	"time"

	"go.platform-mesh.io/apis/terminal/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/subroutines"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	ServiceSubroutineName      = "ServiceSubroutine"
	ServiceSubroutineFinalizer = "terminal.platform-mesh.io/service-finalizer"
	TerminalServicePort        = 8080
	ServiceRequeueAfter        = 5 * time.Second
)

// ServiceSubroutine manages terminal services on the runtime cluster
type ServiceSubroutine struct {
	runtimeClient client.Client
	namespace     string
}

func NewServiceSubroutine(runtimeClient client.Client, namespace string) *ServiceSubroutine {
	return &ServiceSubroutine{
		runtimeClient: runtimeClient,
		namespace:     namespace,
	}
}

func (r *ServiceSubroutine) GetName() string {
	return ServiceSubroutineName
}

func (r *ServiceSubroutine) Finalizers(_ client.Object) []string { // coverage-ignore
	return []string{ServiceSubroutineFinalizer}
}

func (r *ServiceSubroutine) Finalize(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	instance := obj.(*v1alpha1.Terminal)
	log := logger.LoadLoggerFromContext(ctx)

	serviceName := fmt.Sprintf("terminal-%s", instance.Name)
	service := &corev1.Service{}
	serviceKey := client.ObjectKey{Namespace: r.namespace, Name: serviceName}

	if err := r.runtimeClient.Get(ctx, serviceKey, service); err != nil {
		if kerrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			return subroutines.OK(), nil
		}
		return subroutines.OK(), err
	}

	if service.GetDeletionTimestamp() != nil {
		log.Debug().Str("serviceName", service.Name).Msg("service is already being deleted, waiting")
		return subroutines.StopWithRequeue(ServiceRequeueAfter, "service is already being deleted"), nil
	}

	log.Info().Str("serviceName", service.Name).Msg("deleting terminal service")
	if err := r.runtimeClient.Delete(ctx, service); err != nil {
		if kerrors.IsNotFound(err) {
			return subroutines.OK(), nil
		}
		return subroutines.OK(), err
	}

	return subroutines.StopWithRequeue(ServiceRequeueAfter, "terminal service deletion requested"), nil
}

func (r *ServiceSubroutine) Process(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	instance := obj.(*v1alpha1.Terminal)
	log := logger.LoadLoggerFromContext(ctx)

	serviceName := fmt.Sprintf("terminal-%s", instance.Name)

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: r.namespace,
		},
	}

	result, err := controllerutil.CreateOrUpdate(ctx, r.runtimeClient, service, func() error {
		r.mutateService(service, instance)
		return nil
	})
	if err != nil {
		return subroutines.OK(), err
	}

	log.Debug().Str("serviceName", serviceName).Str("result", string(result)).Msg("service reconciled")
	return subroutines.OK(), nil
}

func (r *ServiceSubroutine) mutateService(service *corev1.Service, terminal *v1alpha1.Terminal) {
	service.Labels = map[string]string{
		nameLabel:         nameLabelValue,
		instanceLabel:     terminal.Name,
		managedByLabel:    managedBy,
		terminalNameLabel: terminal.Name,
	}
	service.Spec.Type = corev1.ServiceTypeClusterIP
	service.Spec.Selector = map[string]string{
		terminalNameLabel: terminal.Name,
	}
	service.Spec.Ports = []corev1.ServicePort{
		{
			Name:       "http",
			Port:       TerminalServicePort,
			TargetPort: intstr.FromInt32(TerminalServicePort),
			Protocol:   corev1.ProtocolTCP,
		},
	}
}

var _ subroutines.Processor = (*ServiceSubroutine)(nil)
var _ subroutines.Finalizer = (*ServiceSubroutine)(nil)
