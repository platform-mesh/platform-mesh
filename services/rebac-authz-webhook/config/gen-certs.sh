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

openssl genrsa -out ca.key 2048

openssl req -new -x509 -days 365 -key ca.key \
  -subj "/C=DE/CN=authz-server" -config openssl.conf \
  -out ca.crt

openssl req -newkey rsa:2048 -nodes -keyout tls.key \
  -subj "/C=DE/CN=authz-server" \
  -out tls.csr

# -extfile <(printf "subjectAltName=DNS:host.containers.internal") \
openssl x509 -req \
  -days 365 \
  -extfile <(printf "subjectAltName=DNS:localhost") \
  -in tls.csr \
  -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out tls.crt

# Generate client key and CSR
openssl genrsa -out client.key 2048
openssl req -new -key client.key \
  -subj "/C=DE/CN=authz-client" \
  -out client.csr

# Sign client certificate
openssl x509 -req \
  -days 365 \
  -in client.csr \
  -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out client.crt

rm *.csr


