# Per-chart OCM settings

`hack/release/build-aggregate.sh` publishes an OCM component for every chart
under `charts/`. All of them carry the Platform Mesh version `V` of the build
(the charts in git say `0.0.0`, see `hack/release/stage-charts.sh`). The default
shape is

```
github.com/platform-mesh/<chart>:V                      service component
  ├─ chart → github.com/platform-mesh/helm-charts/<chart>:V
  │            resource "chart" (the packaged chart in the OCI registry)
  │            source   "chart" (this repository at the released commit)
  └─ image → github.com/platform-mesh/images/<chart>:<image version>
```

where the image reference depends on what the chart deploys:

| Chart wraps | `images/<chart>` version | Published by |
|---|---|---|
| a component built in this repository (`chart:` in `ocm/components.yaml`) | `V` (or `--image-version`) | the release workflow, in the same run |
| a component from another repository (portal, iam-ui, …) | the chart's `appVersion` | that repository |
| third-party images listed under `images:` below | `V` | `build-aggregate.sh` itself |
| nothing (`appVersion` empty or `0.0.0`, or `image: false`) | – | – |

A file `ocm/charts/<chart>.yaml` adjusts the shape for one chart. All keys are
optional:

| Key | Effect |
|---|---|
| `publish: false` | push the chart to the registry, but create no OCM component (library charts) |
| `flat: true` | put the chart resource directly into the service component, no `helm-charts/` layer |
| `image: false` | no `image` reference even though `appVersion` is set (charts that only record an upstream version) |
| `images:` | list of OCM resources (third-party images the chart deploys). Published as `github.com/platform-mesh/images/<chart>:V` and referenced as `image` |
| `extraReferences:` | additional `componentReferences` of the service component. Values may use `${VARIABLES}` from `ocm/versions.yaml` |

Charts without such a file use the defaults.
