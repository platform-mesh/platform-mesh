#!/usr/bin/env python3

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

# Reads charts/*/ocm.yaml and third_party/*/ocm.yaml files that contain an
# imageResources section and updates only the imageResources blocks in the
# existing Helm template at charts/platform-mesh-operator/templates/ocm.yaml.
# All other content in that file (Helm guards, enabled: expressions, comments)
# is preserved unchanged.
#
# Usage: python3 hack/generate-ocm-defaults/main.py [charts-dir] [output-file]
#
# Defaults:
#   charts-dir   charts/
#   output-file  charts/platform-mesh-operator/templates/ocm.yaml
#
# third_party/ is always scanned alongside charts-dir.

import os
import re
import sys

import yaml


# Directory names in third_party/ that differ from their profile service names.
THIRD_PARTY_SERVICE_NAME = {
    "cloudnative-pg": "cnpg-operator",
}

SERVICE_NAME_RE = re.compile(r'^[a-z0-9-]+$')


def is_service_name(s: str) -> bool:
    return bool(s) and bool(SERVICE_NAME_RE.match(s))


def load_image_resources(directory: str, name_overrides: dict[str, str]) -> dict[str, str]:
    """Return a map of service name -> indented imageResources YAML block (6-space indent)."""
    result = {}
    if not os.path.isdir(directory):
        return result

    for entry in sorted(os.scandir(directory), key=lambda e: e.name):
        if not entry.is_dir():
            continue
        dir_name = entry.name
        service_name = name_overrides.get(dir_name, dir_name)
        ocm_path = os.path.join(directory, dir_name, "ocm.yaml")
        if not os.path.isfile(ocm_path):
            continue
        with open(ocm_path, encoding="utf-8") as fh:
            data = yaml.safe_load(fh)
        if not data or not data.get("imageResources"):
            continue

        block = yaml.dump(
            {"imageResources": data["imageResources"]},
            default_flow_style=False,
            allow_unicode=True,
            sort_keys=False,
        )
        indented = "".join("      " + line + "\n" for line in block.rstrip("\n").splitlines())
        result[service_name] = indented

    return result


def update_template(template_path: str, image_resources: dict[str, str]) -> None:
    """Rewrite only imageResources blocks in the template; leave everything else alone."""
    with open(template_path, encoding="utf-8") as fh:
        lines = fh.readlines()

    SERVICE_PREFIX = " " * 4
    IMAGE_RESOURCES_PREFIX = " " * 6 + "imageResources:"
    BLOCK_PREFIX = " " * 6

    out = []
    current_service = ""
    skipping = False
    updated = 0

    def flush_service(svc: str) -> None:
        nonlocal current_service, skipping, updated
        if not svc:
            return
        if svc in image_resources:
            out.append(image_resources[svc])
            updated += 1
        current_service = ""
        skipping = False

    def try_service_line(line: str) -> str | None:
        """Return the service name if line is a 4-space-indented service key, else None."""
        stripped = line.rstrip("\n")
        if not stripped.startswith(SERVICE_PREFIX):
            return None
        if stripped.startswith(SERVICE_PREFIX + " "):
            return None
        rest = stripped[len(SERVICE_PREFIX):]
        colon = rest.find(":")
        if colon <= 0:
            return None
        candidate = rest[:colon]
        return candidate if is_service_name(candidate) else None

    for raw_line in lines:
        line = raw_line.rstrip("\n")

        # Detect a new service line (4-space indent, no further indent, valid name).
        svc = try_service_line(line)
        if svc is not None:
            flush_service(current_service)
            current_service = svc
            skipping = False
            out.append(raw_line)
            continue

        # Inside a service block: start skipping on imageResources:.
        if current_service and line.startswith(IMAGE_RESOURCES_PREFIX):
            skipping = True
            continue

        if skipping:
            # Skip lines that are part of the imageResources block (6+ spaces, i.e.
            # the imageResources: header itself and all its list content).
            if line.startswith(BLOCK_PREFIX) or line == "":
                continue
            # Less-indented line: imageResources block is done.
            flush_service(current_service)
            # The current line might itself be a new service.
            svc = try_service_line(line)
            if svc is not None:
                current_service = svc
                out.append(raw_line)
                continue

        out.append(raw_line)

    # Flush any trailing service.
    flush_service(current_service)

    with open(template_path, "w", encoding="utf-8") as fh:
        fh.writelines(out)

    print(f"updated imageResources for {updated} services in {template_path}")


def main() -> None:
    charts_dir = sys.argv[1] if len(sys.argv) > 1 else "charts"
    output_file = sys.argv[2] if len(sys.argv) > 2 else "charts/platform-mesh-operator/templates/ocm.yaml"
    third_party_dir = "third_party"

    image_resources = {}
    image_resources.update(load_image_resources(charts_dir, {}))
    image_resources.update(load_image_resources(third_party_dir, THIRD_PARTY_SERVICE_NAME))

    if not os.path.isfile(output_file):
        print(f"error: output file not found: {output_file}", file=sys.stderr)
        sys.exit(1)

    update_template(output_file, image_resources)


if __name__ == "__main__":
    main()
