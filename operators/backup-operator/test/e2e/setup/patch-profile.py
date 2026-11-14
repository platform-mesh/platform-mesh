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

"""Adds the triton shard and a druid S3 store (one prefix per etcd) to the kind profile."""
import sys

path = sys.argv[1]
text = open(path).read()


def replace_once(old, new):
    global text
    if text.count(old) != 1:
        sys.exit(f"expected exactly one {old!r} in {path}")
    text = text.replace(old, new)


def store(prefix, indent):
    pad = " " * indent
    return (
        f"{pad}backup:\n"
        f"{pad}  store:\n"
        f"{pad}    container: etcd-backups\n"
        f"{pad}    prefix: {prefix}\n"
        f"{pad}    provider: S3\n"
        f"{pad}    endpointOverride: http://backup-s3:4566\n"
        f"{pad}    secretRef:\n"
        f"{pad}      name: etcd-backup-s3\n"
    )


replace_once(
    '            - "root.kcp.{{ .baseDomain }}"\n',
    '            - "root.kcp.{{ .baseDomain }}"\n'
    '            - "triton.kcp.{{ .baseDomain }}"\n',
)
replace_once(
    "              shards: []\n",
    "              shards:\n"
    "                - name: triton\n"
    "                  hostname: triton.kcp.{{ .baseDomain }}\n"
    "                  etcd:\n" + store("etcd-kcp-triton", 20),
)
replace_once(
    "            kcp:\n              external:\n",
    "            kcp:\n"
    "              etcd:\n" + store("etcd-kcp-root", 16) +
    "              cacheServer:\n"
    "                etcd:\n" + store("etcd-cache", 18) +
    "              external:\n",
)
open(path, "w").write(text)
