# jl

jl is a utility cli that can be used to explore json based log files and log streams. It is especially useful when
exploring Kubernetes log streams.

# Installation

## Installation from source
- Checkout this repository
- Use `go install .` from the top-level directory of the clone.

# Usage

```bash
# Show kubernetes log streams
kubectl logs my-pod -n my-namespace --follow | jl

# Show logs stored in a file
jl -i data/input.log

# Show logs stored in a file but skip the level and service properties
jl -i data/input.log -s level,service

# Show logs stored in a file but focus only on the message and reconcile_id property
jl -i data/input.log -f message,reconcile_id

# Show logs stored in a file but focus only on the message property, display only rows that match the select expressions
jl -i data/input.log -rf message,level --select=reconcile_id=fcdc26ae-7feb-4bf2-9058-f0c6666bc356 --select=level=info

```

## Code of Conduct

Please refer to our [Code of Conduct](https://github.com/platform-mesh/.github/blob/main/CODE_OF_CONDUCT.md) for information on the expected conduct for contributing to Platform Mesh.
