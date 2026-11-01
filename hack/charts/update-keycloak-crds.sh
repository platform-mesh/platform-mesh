#!/bin/bash

# Copyright The Platform Mesh Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -euo pipefail

VERSION="${1:?Usage: $0 <keycloak-version>}"
OUT_DIR="charts/keycloak-operator/crds"
BASE_URL="https://raw.githubusercontent.com/k8s-operatorhub/community-operators/main/operators/keycloak-operator/${VERSION}/manifests"

curl -sLf "${BASE_URL}/keycloaks.k8s.keycloak.org-v1.crd.yml" -o "${OUT_DIR}/keycloaks.k8s.keycloak.org-v1.yml"
curl -sLf "${BASE_URL}/keycloakrealmimports.k8s.keycloak.org-v1.crd.yml" -o "${OUT_DIR}/keycloakrealmimports.k8s.keycloak.org-v1.yml"
curl -sLf "${BASE_URL}/keycloakoidcclients.k8s.keycloak.org-v1.crd.yml" -o "${OUT_DIR}/keycloakoidcclients.k8s.keycloak.org-v1.yml"
curl -sLf "${BASE_URL}/keycloaksamlclients.k8s.keycloak.org-v1.crd.yml" -o "${OUT_DIR}/keycloaksamlclients.k8s.keycloak.org-v1.yml"
