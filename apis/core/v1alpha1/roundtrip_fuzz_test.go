package v1alpha1

import (
	"encoding/json"
	"testing"

	"k8s.io/apimachinery/pkg/api/equality"
)

func FuzzAccountRoundTrip(f *testing.F) {
	f.Add([]byte(`{"apiVersion":"core.platform-mesh.io/v1alpha1","kind":"Account","metadata":{"name":"test"},"spec":{"type":"org","displayName":"Test"}}`))
	f.Add([]byte(`{"spec":{"type":"account","displayName":"a","description":"desc","creator":"user","extensions":[{"apiVersion":"v1","kind":"Ext","specGoTemplate":{}}]}}`))
	f.Add([]byte(`{}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		fuzzRoundTrip(t, data, &Account{}, &Account{})
	})
}

func FuzzAccountInfoRoundTrip(f *testing.F) {
	f.Add([]byte(`{"apiVersion":"core.platform-mesh.io/v1alpha1","kind":"AccountInfo","metadata":{"name":"test"},"spec":{"fga":{"store":{"id":"s1"}},"account":{"name":"a","generatedClusterId":"c1","originClusterId":"c2","path":"/p","url":"https://example.com","type":"org"},"organization":{"name":"o","generatedClusterId":"c3","originClusterId":"c4","path":"/o","url":"https://example.com","type":"org"},"clusterInfo":{"ca":"cert"}}}`))
	f.Add([]byte(`{"spec":{"fga":{"store":{"id":""}},"account":{"name":"","generatedClusterId":"","originClusterId":"","path":"","url":"","type":""},"organization":{"name":"","generatedClusterId":"","originClusterId":"","path":"","url":"","type":""},"clusterInfo":{"ca":""},"oidc":{"issuerUrl":"https://auth.example.com","clients":{"app1":{"clientId":"c1"}}}}}`))
	f.Add([]byte(`{}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		fuzzRoundTrip(t, data, &AccountInfo{}, &AccountInfo{})
	})
}

func FuzzStoreRoundTrip(f *testing.F) {
	f.Add([]byte(`{"spec":{"coreModule":"module","tuples":[{"object":"doc:1","relation":"viewer","user":"user:anne"}]}}`))
	f.Add([]byte(`{"status":{"storeID":"s1","authorizationModelID":"am1","managedTuples":[{"object":"o","relation":"r","user":"u"}]}}`))
	f.Add([]byte(`{}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		fuzzRoundTrip(t, data, &Store{}, &Store{})
	})
}

func FuzzAuthorizationModelRoundTrip(f *testing.F) {
	f.Add([]byte(`{"spec":{"storeRef":{"name":"store","cluster":"cl1"},"model":"model openfga/v1","tuples":[{"object":"doc:1","relation":"viewer","user":"user:anne"}]}}`))
	f.Add([]byte(`{"status":{"managedTuples":[{"object":"o","relation":"r","user":"u"}]}}`))
	f.Add([]byte(`{}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		fuzzRoundTrip(t, data, &AuthorizationModel{}, &AuthorizationModel{})
	})
}

func FuzzAPIExportPolicyRoundTrip(f *testing.F) {
	f.Add([]byte(`{"spec":{"apiExportRef":{"name":"export","clusterPath":"root:org"},"allowPathExpressions":["root:org:*"]}}`))
	f.Add([]byte(`{"status":{"managedAllowExpressions":["root:org:ws1"]}}`))
	f.Add([]byte(`{}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		fuzzRoundTrip(t, data, &APIExportPolicy{}, &APIExportPolicy{})
	})
}

func FuzzIdentityProviderConfigurationRoundTrip(f *testing.F) {
	f.Add([]byte(`{"spec":{"registrationAllowed":true,"clients":[{"clientType":"confidential","clientName":"app","redirectURIs":["https://app/callback"]}]}}`))
	f.Add([]byte(`{"status":{"managedClients":{"app":{"clientID":"c1","registrationClientURI":"https://kc/clients/c1"}}}}`))
	f.Add([]byte(`{}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		fuzzRoundTrip(t, data, &IdentityProviderConfiguration{}, &IdentityProviderConfiguration{})
	})
}

func FuzzInviteRoundTrip(f *testing.F) {
	f.Add([]byte(`{"spec":{"email":"user@example.com"}}`))
	f.Add([]byte(`{"spec":{"email":""}}`))
	f.Add([]byte(`{}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		fuzzRoundTrip(t, data, &Invite{}, &Invite{})
	})
}

// fuzzRoundTrip unmarshals arbitrary JSON into obj, marshals it back, unmarshals
// into obj2, and checks semantic equality. We use equality.Semantic.DeepEqual from
// k8s.io/apimachinery which treats nil and empty slices/maps as equivalent — the
// standard Kubernetes comparison semantic for API objects.
func fuzzRoundTrip[T any](t *testing.T, data []byte, obj *T, obj2 *T) {
	t.Helper()

	if err := json.Unmarshal(data, obj); err != nil {
		return
	}

	roundtripped, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	if err := json.Unmarshal(roundtripped, obj2); err != nil {
		t.Fatalf("failed to unmarshal roundtripped data: %v", err)
	}

	if !equality.Semantic.DeepEqual(obj, obj2) {
		t.Errorf("roundtrip mismatch for %T", obj)
	}
}
