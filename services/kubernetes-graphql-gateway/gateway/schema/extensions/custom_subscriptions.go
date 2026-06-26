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

package extensions

import (
	"bufio"
	"fmt"

	"github.com/graphql-go/graphql"

	"go.platform-mesh.io/kubernetes-graphql-gateway/apischema"
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/resolver"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/kube-openapi/pkg/validation/spec"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	podLogsFieldName = "podLogs"
	containerArg     = "container"
	tailLinesArg     = "tailLines"
	sinceSecondsArg  = "sinceSeconds"
	followArg        = "follow"
)

type PodLogEntry struct {
	Message   string `json:"message"`
	Container string `json:"container"`
}

type CustomSubscriptionGenerator struct {
	clientset kubernetes.Interface
}

func NewCustomSubscriptionGenerator(restCfg *rest.Config) (*CustomSubscriptionGenerator, error) {
	cs, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes clientset: %w", err)
	}
	return &CustomSubscriptionGenerator{clientset: cs}, nil
}

func (g *CustomSubscriptionGenerator) AddPodLogsSubscription(rootSubscription *graphql.Object, definitions map[string]*spec.Schema) {
	if !hasPodResource(definitions) {
		return
	}

	podLogEntryType := graphql.NewObject(graphql.ObjectConfig{
		Name: "PodLogEntry",
		Fields: graphql.Fields{
			"message":   &graphql.Field{Type: graphql.NewNonNull(graphql.String)},
			"container": &graphql.Field{Type: graphql.NewNonNull(graphql.String)},
		},
	})

	rootSubscription.AddFieldConfig(podLogsFieldName, &graphql.Field{
		Type: graphql.NewNonNull(podLogEntryType),
		Args: graphql.FieldConfigArgument{
			resolver.NameArg: resolver.NameArgConfig,
			resolver.NamespaceArg: &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "Namespace of the pod",
			},
			containerArg: &graphql.ArgumentConfig{
				Type:        graphql.String,
				Description: "Container name (required for multi-container pods)",
			},
			tailLinesArg: &graphql.ArgumentConfig{
				Type:        graphql.Int,
				Description: "Number of lines from the end of the log to start streaming",
			},
			sinceSecondsArg: &graphql.ArgumentConfig{
				Type:        graphql.Int,
				Description: "Only return logs newer than this many seconds",
			},
			followArg: &graphql.ArgumentConfig{
				Type:         graphql.Boolean,
				DefaultValue: true,
				Description:  "Whether to stream new log entries as they arrive",
			},
		},
		Resolve: func(p graphql.ResolveParams) (any, error) {
			if err, ok := p.Source.(error); ok {
				return nil, err
			}
			return p.Source, nil
		},
		Subscribe: g.subscribePodLogs(),
	})
}

func (g *CustomSubscriptionGenerator) subscribePodLogs() graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		name, err := resolver.GetArg[string](p.Args, resolver.NameArg, true)
		if err != nil {
			return nil, err
		}

		namespace, err := resolver.GetArg[string](p.Args, resolver.NamespaceArg, true)
		if err != nil {
			return nil, err
		}

		container, err := resolver.GetArg[string](p.Args, containerArg, false)
		if err != nil {
			return nil, err
		}

		tailLines, err := resolver.GetArg[int](p.Args, tailLinesArg, false)
		if err != nil {
			return nil, err
		}

		sinceSeconds, err := resolver.GetArg[int](p.Args, sinceSecondsArg, false)
		if err != nil {
			return nil, err
		}

		follow, err := resolver.GetArg[bool](p.Args, followArg, false)
		if err != nil {
			return nil, err
		}

		opts := &corev1.PodLogOptions{
			Follow: follow,
		}

		if container != "" {
			opts.Container = container
		}

		if tailLines > 0 {
			tl := int64(tailLines)
			opts.TailLines = &tl
		}

		if sinceSeconds > 0 {
			ss := int64(sinceSeconds)
			opts.SinceSeconds = &ss
		}

		stream, err := g.clientset.CoreV1().Pods(namespace).GetLogs(name, opts).Stream(p.Context)
		if err != nil {
			return nil, fmt.Errorf("failed to stream pod logs: %w", err)
		}

		resultChannel := make(chan any)

		go func() {
			defer close(resultChannel)
			defer func() { _ = stream.Close() }()

			logger := log.FromContext(p.Context).WithValues(
				"operation", "podLogs",
				"pod", name,
				"namespace", namespace,
			)

			scanner := bufio.NewScanner(stream)
			for scanner.Scan() {
				entry := PodLogEntry{
					Message:   scanner.Text(),
					Container: container,
				}

				select {
				case <-p.Context.Done():
					return
				case resultChannel <- entry:
				}
			}

			if err := scanner.Err(); err != nil {
				logger.Error(err, "Error reading pod log stream")
				select {
				case <-p.Context.Done():
				case resultChannel <- fmt.Errorf("error reading pod log stream: %w", err):
				}
			}
		}()

		return resultChannel, nil
	}
}

func hasPodResource(definitions map[string]*spec.Schema) bool {
	for _, def := range definitions {
		gvk, err := apischema.ExtractGVK(def)
		if err != nil || gvk == nil {
			continue
		}
		if gvk.Group == "" && gvk.Version == "v1" && gvk.Kind == "Pod" {
			return true
		}
	}
	return false
}
