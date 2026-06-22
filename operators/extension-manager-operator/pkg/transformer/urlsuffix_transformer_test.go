package transformer

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"go.platform-mesh.io/apis/ui/v1alpha1"
	"go.platform-mesh.io/extension-manager-operator/pkg/validation"
)

func TestUrlSuffixTransformer_Transform(t *testing.T) {
	transformer := &UrlSuffixTransformer{}
	tests := []struct {
		name          string
		before        *validation.ContentConfiguration
		instance      *v1alpha1.ContentConfiguration
		expected      *validation.ContentConfiguration
		expectedError bool
	}{
		{
			name:          "Test UrlSuffixTransformer Transform",
			expectedError: false,
			instance:      &v1alpha1.ContentConfiguration{Spec: v1alpha1.ContentConfigurationSpec{RemoteConfiguration: &v1alpha1.RemoteConfiguration{URL: "https://test.com:9999/ui/cdm/config.json"}}},
			before: &validation.ContentConfiguration{
				LuigiConfigFragment: validation.LuigiConfigFragment{
					Data: validation.LuigiConfigData{
						Nodes: []validation.Node{
							{
								UrlSuffix: "test/#/my-ui?query=param&query2=param2",
								Children: []validation.Node{
									{
										UrlSuffix: "test/#/my-child-1?query=param&query3=param4",
									},
									{
										UrlSuffix: "test/#/my-child-2?query=param&query1=param5",
									},
								},
							},
						},
					},
				},
			},
			expected: &validation.ContentConfiguration{
				LuigiConfigFragment: validation.LuigiConfigFragment{
					Data: validation.LuigiConfigData{
						Nodes: []validation.Node{
							{
								Url:       "https://test.com:9999/test/#/my-ui?query=param&query2=param2",
								UrlSuffix: "",
								Children: []validation.Node{
									{
										UrlSuffix: "",
										Url:       "https://test.com:9999/test/#/my-child-1?query=param&query3=param4",
									},
									{
										UrlSuffix: "",
										Url:       "https://test.com:9999/test/#/my-child-2?query=param&query1=param5",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:          "Test UrlSuffixTransformer Transform with Inline Config",
			expectedError: false,
			instance:      &v1alpha1.ContentConfiguration{Spec: v1alpha1.ContentConfigurationSpec{InlineConfiguration: &v1alpha1.InlineConfiguration{Content: "test"}}},
			before: &validation.ContentConfiguration{
				LuigiConfigFragment: validation.LuigiConfigFragment{
					Data: validation.LuigiConfigData{
						Nodes: []validation.Node{
							{
								UrlSuffix: "test/#/my-ui?query=param&query2=param2",
								Children: []validation.Node{
									{
										UrlSuffix: "test/#/my-child-1?query=param&query3=param4",
									},
									{
										UrlSuffix: "test/#/my-child-2?query=param&query1=param5",
									},
								},
							},
						},
					},
				},
			},
			expected: &validation.ContentConfiguration{
				LuigiConfigFragment: validation.LuigiConfigFragment{
					Data: validation.LuigiConfigData{
						Nodes: []validation.Node{
							{
								UrlSuffix: "test/#/my-ui?query=param&query2=param2",
								Children: []validation.Node{
									{
										UrlSuffix: "test/#/my-child-1?query=param&query3=param4",
									},
									{
										UrlSuffix: "test/#/my-child-2?query=param&query1=param5",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := transformer.Transform(tc.before, tc.instance)
			if tc.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.before, tc.expected)
			}
		})

	}
}
