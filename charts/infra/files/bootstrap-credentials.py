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

"""Create missing infra credential Secrets without rotating existing passwords."""

import base64
import json
import os
from pathlib import Path
import secrets
import ssl
import sys
import time
import urllib.error
import urllib.parse
import urllib.request


class CredentialError(Exception):
    pass


class KubernetesAPI:
    def __init__(self, namespace):
        account = Path("/var/run/secrets/kubernetes.io/serviceaccount")
        self.token = (account / "token").read_text().strip()
        self.tls = ssl.create_default_context(cafile=str(account / "ca.crt"))
        host = os.environ["KUBERNETES_SERVICE_HOST"]
        if ":" in host:
            host = f"[{host}]"
        port = os.environ.get("KUBERNETES_SERVICE_PORT_HTTPS", "443")
        self.url = f"https://{host}:{port}/api/v1/namespaces/{urllib.parse.quote(namespace, safe='')}/secrets"

    def request(self, method, name=None, body=None):
        url = self.url + ("/" + urllib.parse.quote(name, safe="") if name else "")
        data = json.dumps(body).encode() if body is not None else None
        request = urllib.request.Request(
            url, data=data, method=method,
            headers={"Authorization": "Bearer " + self.token, "Content-Type": "application/json"},
        )
        for attempt in range(5):
            try:
                with urllib.request.urlopen(request, context=self.tls, timeout=10) as response:
                    return json.load(response)
            except urllib.error.HTTPError as error:
                if method == "GET" and error.code == 404:
                    return None
                if method == "POST" and error.code == 409:
                    return None  # Another bootstrap Job may have created it.
                if error.code not in (429, 500, 502, 503, 504) or attempt == 4:
                    # API error bodies can contain submitted Secret data.
                    raise CredentialError(f"Kubernetes {method} failed with HTTP {error.code}") from None
            except (urllib.error.URLError, TimeoutError):
                if attempt == 4:
                    raise CredentialError("Kubernetes API request failed") from None
            time.sleep(min(2 ** attempt, 5))

    def get(self, name):
        return self.request("GET", name)

    def create(self, name, data):
        body = {
            "apiVersion": "v1", "kind": "Secret", "type": "Opaque",
            # No chart tracking or owner reference: credentials outlive the Job/release.
            "metadata": {"name": name},
            "data": {key: base64.b64encode(value.encode()).decode() for key, value in data.items()},
        }
        if self.request("POST", body=body) is None:
            existing = self.get(name)
            if existing is None or decode_data(existing) != data:
                raise CredentialError(f"Secret {name} was created concurrently; retry bootstrap")


def decode_data(secret):
    try:
        return {key: base64.b64decode(value, validate=True).decode() for key, value in secret.get("data", {}).items()}
    except (ValueError, UnicodeError):
        raise CredentialError(f"Secret {secret['metadata']['name']} contains invalid credential data") from None


def bootstrap(api, groups):
    pending = []
    # Validate every group before writing, including partial installations.
    for group in groups:
        password = None
        username = None
        missing = []
        for spec in group:
            name = spec["name"]
            if username is not None and username != spec["username"]:
                raise CredentialError("Keycloak and CNPG Keycloak usernames must match")
            username = spec["username"]
            candidate = spec.get("password")
            # Explicit overrides are rendered by the normal Secret templates.
            if not candidate:
                existing = api.get(name)
                if existing is None:
                    if spec.get("required"):
                        raise CredentialError(f"Secret {name} must exist before database bootstrap")
                    if not spec.get("readOnly"):
                        missing.append(spec)
                else:
                    data = decode_data(existing)
                    if data.get("username") != username or not data.get("password"):
                        raise CredentialError(f"Secret {name} requires the configured username and a non-empty password")
                    candidate = data["password"]
            if candidate:
                if password is not None and password != candidate:
                    raise CredentialError("Keycloak and CNPG Keycloak passwords must match; existing Secrets were not changed")
                password = candidate
        if missing:
            password = password or secrets.token_hex(32)
            for spec in missing:
                data = {"username": spec["username"], "password": password}
                pending.append((spec["name"], data))
    for name, data in pending:
        api.create(name, data)


def main():
    config = json.loads(Path("/bootstrap/config.json").read_text())
    bootstrap(KubernetesAPI(os.environ["POD_NAMESPACE"]), config["groups"])
    print("Credential bootstrap completed")


if __name__ == "__main__":
    try:
        main()
    except CredentialError as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
    except Exception:
        # Never emit a traceback containing API requests or credential values.
        print("Credential bootstrap failed", file=sys.stderr)
        sys.exit(1)
