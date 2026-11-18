# Copyright The Platform Mesh Authors.
# SPDX-License-Identifier: Apache-2.0

import base64
from copy import deepcopy
import importlib.util
from pathlib import Path
import unittest

module_spec = importlib.util.spec_from_file_location(
    "bootstrap_credentials", Path(__file__).parents[1] / "files/bootstrap-credentials.py"
)
bootstrap_module = importlib.util.module_from_spec(module_spec)
module_spec.loader.exec_module(bootstrap_module)


def stored_secret(name, username, password, **extra):
    data = {"username": username, "password": password, **extra}
    return {
        "metadata": {"name": name},
        "data": {key: base64.b64encode(value.encode()).decode() for key, value in data.items()},
    }


def default_groups():
    return [
        [{"name": "cnpg-keycloak-user", "username": "keycloak"},
         {"name": "keycloak-db-credentials", "username": "keycloak", "readOnly": True}],
        [{"name": "cnpg-openfga-user", "username": "openfga"}],
    ]


class FakeAPI:
    def __init__(self, stored=None):
        self.stored = deepcopy(stored or {})
        self.created = []
        self.read = []

    def get(self, name):
        self.read.append(name)
        return self.stored.get(name)

    def create(self, name, data):
        assert name not in self.stored
        self.created.append(name)
        self.stored[name] = stored_secret(name, **data)


class BootstrapTest(unittest.TestCase):
    def test_creates_only_independent_database_credentials_and_preserves_them(self):
        api = FakeAPI()
        groups = default_groups()
        original = deepcopy(groups)
        bootstrap_module.bootstrap(api, groups)
        self.assertEqual(set(api.created), {"cnpg-keycloak-user", "cnpg-openfga-user"})
        data = {name: bootstrap_module.decode_data(secret) for name, secret in api.stored.items()}
        for secret in data.values():
            self.assertEqual(set(secret), {"username", "password"})
            self.assertRegex(secret["password"], r"^[a-f0-9]{64}$")
        self.assertNotEqual(data["cnpg-keycloak-user"]["password"], data["cnpg-openfga-user"]["password"])
        before = deepcopy(api.stored)
        api.created.clear()
        bootstrap_module.bootstrap(api, groups)
        self.assertEqual(api.stored, before)
        self.assertEqual(api.created, [])
        self.assertEqual(groups, original)
        self.assertNotIn("keycloak-admin", api.read)
        self.assertNotIn("keycloak-initial-admin", api.read)

    def test_preserves_legacy_passwords_and_partial_installations(self):
        for name in ("keycloak-db-credentials", "cnpg-keycloak-user"):
            with self.subTest(name=name):
                existing = stored_secret(name, "keycloak", "legacy-password")
                api = FakeAPI({name: existing})
                bootstrap_module.bootstrap(api, default_groups())
                self.assertEqual(api.stored[name], existing)
                self.assertEqual(bootstrap_module.decode_data(api.stored["cnpg-keycloak-user"])["password"], "legacy-password")
                if name == "cnpg-keycloak-user":
                    self.assertNotIn("keycloak-db-credentials", api.stored)

    def test_legacy_password_override_seeds_role_without_creating_legacy_secret(self):
        groups = default_groups()
        groups[0][1]["password"] = "explicit-password"
        api = FakeAPI()
        bootstrap_module.bootstrap(api, groups)
        self.assertEqual(bootstrap_module.decode_data(api.stored["cnpg-keycloak-user"])["password"], "explicit-password")
        self.assertNotIn("keycloak-db-credentials", api.stored)

    def test_explicit_role_overrides_are_left_to_chart_templates(self):
        groups = default_groups()
        groups[0][0]["password"] = "keycloak-override"
        groups[1][0]["password"] = "openfga-override"
        api = FakeAPI()
        bootstrap_module.bootstrap(api, groups)
        self.assertEqual(api.created, [])

    def test_supplied_database_secret_is_read_only_and_seeds_missing_role(self):
        groups = default_groups()
        groups[0][1]["name"] = "supplied-keycloak-db"
        existing = stored_secret("supplied-keycloak-db", "keycloak", "supplied-password")
        api = FakeAPI({"supplied-keycloak-db": existing})
        bootstrap_module.bootstrap(api, groups)
        self.assertEqual(api.stored["supplied-keycloak-db"], existing)
        self.assertEqual(bootstrap_module.decode_data(api.stored["cnpg-keycloak-user"])["password"], "supplied-password")
        self.assertNotIn("supplied-keycloak-db", api.created)

    def test_missing_supplied_credentials_fail_before_any_writes(self):
        for index in (0, 1):
            with self.subTest(index=index):
                groups = default_groups()
                groups[0][index]["required"] = True
                api = FakeAPI()
                with self.assertRaises(bootstrap_module.CredentialError):
                    bootstrap_module.bootstrap(api, groups)
                self.assertEqual(api.created, [])

    def test_conflicts_and_incomplete_secrets_fail_before_any_writes(self):
        cases = [
            {"keycloak-db-credentials": stored_secret("keycloak-db-credentials", "keycloak", "first-password"),
             "cnpg-keycloak-user": stored_secret("cnpg-keycloak-user", "keycloak", "second-password")},
            {"cnpg-keycloak-user": stored_secret("cnpg-keycloak-user", "wrong-user", "invalid-user-credential")},
            {"cnpg-openfga-user": stored_secret("cnpg-openfga-user", "openfga", "")},
            {"keycloak-db-credentials": stored_secret("keycloak-db-credentials", "keycloak", "")},
        ]
        for existing in cases:
            with self.subTest(existing=list(existing)):
                api = FakeAPI(existing)
                with self.assertRaises(bootstrap_module.CredentialError) as error:
                    bootstrap_module.bootstrap(api, default_groups())
                for secret in existing.values():
                    password = bootstrap_module.decode_data(secret)["password"]
                    if password:
                        self.assertNotIn(password, str(error.exception))
                self.assertEqual(api.stored, existing)
                self.assertEqual(api.created, [])

    def test_conflicting_overrides_fail_before_any_writes(self):
        groups = default_groups()
        groups[0][0]["password"] = "role-override"
        groups[0][1]["password"] = "db-override"
        api = FakeAPI()
        with self.assertRaises(bootstrap_module.CredentialError):
            bootstrap_module.bootstrap(api, groups)
        self.assertEqual(api.created, [])

    def test_mismatched_configured_usernames_fail_before_any_writes(self):
        groups = default_groups()
        groups[0][1]["username"] = "other-user"
        api = FakeAPI()
        with self.assertRaises(bootstrap_module.CredentialError):
            bootstrap_module.bootstrap(api, groups)
        self.assertEqual(api.created, [])


if __name__ == "__main__":
    unittest.main()
