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

// Command module-app is the workload of the e2e test module. It reports the
// context the deployer handed it, and reads a ConfigMap out of its own kcp
// workspace so a test can prove the minted kubeconfig really works.
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// identity is what the deployer is expected to have provided: the values come
// from the generated ConfigMap, mounted with envFrom.
type identity struct {
	OCMModule    string `json:"module"`
	Component    string `json:"component"`
	Placement    string `json:"placement"`
	Cluster      string `json:"cluster"`
	PlatformMesh string `json:"platformMesh"`
	ShardGroup   string `json:"shardGroup,omitempty"`
	Workspace    string `json:"workspace,omitempty"`
	// Greeting is templated into the manifest from spec.values, proving
	// value substitution reached the payload.
	Greeting string `json:"greeting,omitempty"`
	// Secret is read from a ConfigMap in the module's kcp workspace, which
	// only works if the mounted kubeconfig and its RBAC are correct.
	Secret string `json:"secret,omitempty"`
	// Error reports why the kcp lookup failed, so a test failure is
	// diagnosable from the response.
	Error string `json:"error,omitempty"`
}

func main() {
	id := identity{
		OCMModule:    os.Getenv("MODULE"),
		Component:    os.Getenv("COMPONENT"),
		Placement:    os.Getenv("PLACEMENT"),
		Cluster:      os.Getenv("CLUSTER"),
		PlatformMesh: os.Getenv("PLATFORM_MESH"),
		ShardGroup:   os.Getenv("SHARD_GROUP"),
		Workspace:    os.Getenv("WORKSPACE"),
		Greeting:     os.Getenv("GREETING"),
	}
	log.Printf("module-app up: %+v", id)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Read on request, not at start-up: the test writes the
		// ConfigMap after the workload is already running.
		out := id
		out.Secret, out.Error = readSecret(r.Context())

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(out); err != nil {
			log.Printf("writing response: %v", err)
		}
	})

	addr := ":8080"
	if port := os.Getenv("PORT"); port != "" {
		addr = ":" + port
	}
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	// When the deployer mapped this component into the front proxy it also
	// minted a serving certificate; the front proxy only talks TLS.
	cert, key := os.Getenv("TLS_CERT_FILE"), os.Getenv("TLS_KEY_FILE")
	if cert != "" && key != "" {
		log.Printf("serving https on %s", addr)
		log.Fatal(server.ListenAndServeTLS(cert, key))
	}
	log.Printf("serving http on %s", addr)
	log.Fatal(server.ListenAndServe())
}

// readSecret fetches the value the test placed in the module's workspace.
// KUBECONFIG_PATH, SECRET_NAMESPACE and SECRET_NAME come from the manifest.
func readSecret(ctx context.Context) (string, string) {
	path := os.Getenv("KUBECONFIG_PATH")
	if path == "" {
		return "", ""
	}

	cfg, err := clientcmd.BuildConfigFromFlags("", path)
	if err != nil {
		return "", "building kcp config: " + err.Error()
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return "", "building kcp client: " + err.Error()
	}

	namespace := os.Getenv("SECRET_NAMESPACE")
	if namespace == "" {
		namespace = "default"
	}
	name := os.Getenv("SECRET_NAME")

	cm, err := client.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", "reading ConfigMap from kcp: " + err.Error()
	}
	return cm.Data["value"], ""
}
