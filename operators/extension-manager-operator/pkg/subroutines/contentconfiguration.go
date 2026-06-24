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
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	pmuiv1alpha1 "go.platform-mesh.io/apis/ui/v1alpha1"
	"go.platform-mesh.io/extension-manager-operator/pkg/transformer"
	"go.platform-mesh.io/extension-manager-operator/pkg/validation"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/subroutines"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ContentConfigurationSubroutineName = "ContentConfigurationSubroutine"
	ValidationConditionType            = "Valid"
	ValidationConditionReasonSuccess   = "ValidationSucceeded"
	ValidationConditionReasonFailed    = "ValidationFailed"
	ConditionStatusTrue                = "True"
	ConditionStatusFalse               = "False"
)

var _ subroutines.Processor = (*ContentConfigurationSubroutine)(nil)

type ContentConfigurationSubroutine struct {
	client      *http.Client
	validator   validation.ExtensionConfiguration
	transformer []transformer.ContentConfigurationTransformer
}

func NewContentConfigurationSubroutine(validator validation.ExtensionConfiguration, client *http.Client) *ContentConfigurationSubroutine {
	transformers := []transformer.ContentConfigurationTransformer{
		&transformer.UrlSuffixTransformer{},
	}
	return &ContentConfigurationSubroutine{
		client:      client,
		validator:   validator,
		transformer: transformers,
	}
}

func (r *ContentConfigurationSubroutine) GetName() string {
	return ContentConfigurationSubroutineName
}

func (r *ContentConfigurationSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	log := logger.LoadLoggerFromContext(ctx)

	instance, ok := obj.(*pmuiv1alpha1.ContentConfiguration)
	if !ok {
		return subroutines.OK(), fmt.Errorf("expected *v1alpha1.ContentConfiguration, got %T", obj)
	}

	log.Debug().Str("name", instance.Name).Msg("processing content configuration")

	// Download or Retrieve ContentConfiguration Json
	contentType, rawConfig, err := r.retrieveContentConfigurationData(instance, log)
	if err != nil {
		return subroutines.OK(), err
	}

	// Validate ContentConfiguration Json
	validatedConfig, valErr := r.validator.Validate(rawConfig, contentType)
	if valErr != nil && valErr.Len() > 0 {
		log.Err(valErr).Msg("failed to validate configuration")
		condition := metav1.Condition{
			Type:    ValidationConditionType,
			Status:  ConditionStatusFalse,
			Reason:  ValidationConditionReasonFailed,
			Message: valErr.Error(),
		}
		meta.SetStatusCondition(&instance.Status.Conditions, condition)
		return subroutines.OK(), nil
	}

	condition := metav1.Condition{
		Type:    ValidationConditionType,
		Status:  ConditionStatusTrue,
		Reason:  ValidationConditionReasonSuccess,
		Message: "OK",
	}
	meta.SetStatusCondition(&instance.Status.Conditions, condition)

	// Transform ContentConfiguration Json
	contentConfiguration := &validation.ContentConfiguration{}
	err = json.Unmarshal([]byte(validatedConfig), contentConfiguration)
	if err != nil {
		return subroutines.OK(), fmt.Errorf("failed to unmarshal contentConfiguration: %w", err)
	}
	for _, configurationTransformer := range r.transformer {
		err := configurationTransformer.Transform(contentConfiguration, instance)
		if err != nil {
			return subroutines.OK(), fmt.Errorf("failed to transform contentConfiguration: %w", err)
		}
	}

	validatedConfigBytes, err := json.Marshal(contentConfiguration)
	if err != nil {
		return subroutines.OK(), fmt.Errorf("failed to marshal contentConfiguration: %w", err)
	}
	validatedConfig = string(validatedConfigBytes)

	// Store resulting configuration in the status
	instance.Status.ConfigurationResult = validatedConfig
	return subroutines.OK(), nil
}

func (r *ContentConfigurationSubroutine) retrieveContentConfigurationData(instance *pmuiv1alpha1.ContentConfiguration, log *logger.Logger) (string, []byte, error) {
	var contentType string
	var rawConfig []byte
	// InlineConfiguration has higher priority than RemoteConfiguration
	switch {
	case instance.Spec.InlineConfiguration != nil && instance.Spec.InlineConfiguration.Content != "":
		contentType = instance.Spec.InlineConfiguration.ContentType
		rawConfig = []byte(instance.Spec.InlineConfiguration.Content)
	case instance.Spec.RemoteConfiguration != nil && instance.Spec.RemoteConfiguration.URL != "":
		url := instance.Spec.RemoteConfiguration.URL
		if instance.Spec.RemoteConfiguration.InternalUrl != "" {
			url = instance.Spec.RemoteConfiguration.InternalUrl
		}
		bytes, err := r.getRemoteConfig(url, log)
		if err != nil {
			log.Err(err).Msg("failed to fetch remote configuration")
			return "", nil, err
		}
		log.Info().Msg("fetched remote configuration")
		contentType = instance.Spec.RemoteConfiguration.ContentType
		rawConfig = bytes
	default:
		return "", nil, fmt.Errorf("no configuration provided")
	}
	return contentType, rawConfig, nil
}

// Do makes an HTTP request to the specified URL.
func (r *ContentConfigurationSubroutine) getRemoteConfig(url string, log *logger.Logger) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %v", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Err(closeErr).Msg("failed to close response body")
		}
	}()

	if resp.StatusCode != http.StatusOK {
		// Give the caller signal to retry if we have 5xx status codes
		if resp.StatusCode >= http.StatusInternalServerError && resp.StatusCode < 600 {
			return nil, fmt.Errorf("received non-200 status code: %d", resp.StatusCode)
		}

		return nil, fmt.Errorf("received non-200 status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %v", err)
	}
	// TODO
	// we need to check the size of the received body before loading it to memory.
	// In case it exceeds a certain size we should reject it.
	// https://go.platform-mesh.io/extension-manager-operator/pull/23#discussion_r1622598363

	return body, nil
}
